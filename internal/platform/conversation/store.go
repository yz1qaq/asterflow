package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yz1qaq/asterflow/internal/domain"
	pgstore "github.com/yz1qaq/asterflow/internal/infrastructure/postgres/sqlc"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
)

var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrTurnBusy             = errors.New("conversation turn is already running")
	ErrTurnLost             = errors.New("conversation turn token is no longer valid")
)

type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db, q: pgstore.New(db)}
}

func conversationFromRow(row pgstore.Conversation) *domain.Conversation {
	return &domain.Conversation{
		ID: row.ID.String(), WorkspaceID: row.WorkspaceID,
		AgentID: row.AgentID.String(), AgentVersionID: row.AgentVersionID.String(),
		UserID: row.UserID, Status: row.Status, Classification: row.Classification,
		RuntimeConfigJSON: string(row.RuntimeConfigJson),
		StartedAt:         row.StartedAt, UpdatedAt: row.UpdatedAt,
		TurnRevision: row.TurnRevision,
	}
}

func (s *PostgresStore) CreateConversation(ctx context.Context, c *domain.Conversation) error {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	agentID, err := uuid.Parse(c.AgentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	versionID, err := uuid.Parse(c.AgentVersionID)
	if err != nil {
		return fmt.Errorf("parse agent version id: %w", err)
	}
	config := c.RuntimeConfigJSON
	if config == "" {
		config = "{}"
	}
	row, err := s.q.CreateConversation(ctx, pgstore.CreateConversationParams{
		ID: id, WorkspaceID: c.WorkspaceID, AgentID: agentID, AgentVersionID: versionID,
		UserID: c.UserID, Status: orStatus(c.Status), Classification: orClassification(c.Classification),
		RuntimeConfigJson: []byte(config),
	})
	if err != nil {
		var violation *pgconn.PgError
		if errors.As(err, &violation) && violation.Code == "23503" {
			// The Agent or its version can disappear between lookup and insert.
			return ErrConversationNotFound
		}
		return fmt.Errorf("create conversation: %w", err)
	}
	*c = *conversationFromRow(row)
	return nil
}

func orStatus(status string) string {
	if status == "" {
		return "active"
	}
	return status
}
func orClassification(c string) string {
	if c == "" {
		return "internal"
	}
	return c
}

func (s *PostgresStore) GetConversation(ctx context.Context, workspaceID, id string) (*domain.Conversation, error) {
	cid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}
	row, err := s.q.GetConversation(ctx, pgstore.GetConversationParams{ID: cid, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return conversationFromRow(row), nil
}

// DeleteOwnedConversation checks ownership and the active lease in the same
// statement as the deletion. The foreign keys cascade to messages and tool calls.
func (s *PostgresStore) DeleteOwnedConversation(ctx context.Context, workspaceID, conversationID, userID string) error {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	affected, err := s.q.DeleteOwnedConversation(ctx, pgstore.DeleteOwnedConversationParams{
		ID: id, WorkspaceID: workspaceID, UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if affected == 1 {
		return nil
	}

	conv, err := s.GetConversation(ctx, workspaceID, conversationID)
	if err != nil {
		return err
	}
	if conv.UserID != userID {
		return ErrConversationNotFound
	}
	return ErrTurnBusy
}

func (s *PostgresStore) ListConversations(ctx context.Context, workspaceID, userID string, limit, offset int32) ([]*domain.Conversation, error) {
	var user pgtype.Text
	if userID != "" {
		user = pgtype.Text{String: userID, Valid: true}
	}

	rows, err := s.q.ListConversations(ctx, pgstore.ListConversationsParams{
		WorkspaceID: workspaceID, UserID: user, RowLimit: limit, RowOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	out := make([]*domain.Conversation, 0, len(rows))
	for _, row := range rows {
		out = append(out, conversationFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) ListMessages(ctx context.Context, workspaceID, conversationID string) ([]*domain.Message, error) {
	cid, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}
	rows, err := s.q.ListConversationMessages(ctx, pgstore.ListConversationMessagesParams{
		ConversationID: cid, WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	messages := make([]*domain.Message, 0, len(rows))
	for _, row := range rows {
		var calls []domain.ToolCall
		if err := json.Unmarshal(row.ToolCalls, &calls); err != nil {
			return nil, fmt.Errorf("decode message tool calls: %w", err)
		}
		message := &domain.Message{
			ID: row.ID.String(), ConversationID: row.ConversationID.String(),
			Role: row.Role, Content: row.Content, ToolCalls: calls, CreatedAt: row.CreatedAt,
		}
		if row.ToolCallID.Valid {
			id := row.ToolCallID.String
			message.ToolCallID = &id
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// ClaimTurn acquires the single-turn lease; ErrTurnBusy means another turn is running.
func (s *PostgresStore) ClaimTurn(ctx context.Context, workspaceID, conversationID string, leaseSeconds int32) (string, int64, error) {
	cid, err := uuid.Parse(conversationID)
	if err != nil {
		return "", 0, fmt.Errorf("parse conversation id: %w", err)
	}
	token := uuid.New()
	//SQL抢占租约
	row, err := s.q.ClaimConversationTurn(ctx, pgstore.ClaimConversationTurnParams{
		TurnToken: token, LeaseSeconds: leaseSeconds, ID: cid, WorkspaceID: workspaceID,
	})
	//如果没抢到
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.GetConversation(ctx, workspaceID, conversationID); err != nil {
			return "", 0, err
		}
		return "", 0, ErrTurnBusy
	}
	if err != nil {
		return "", 0, fmt.Errorf("claim conversation turn: %w", err)
	}
	return token.String(), row.TurnRevision, nil
}

func (s *PostgresStore) RenewTurn(ctx context.Context, workspaceID, conversationID, token string, leaseSeconds int32) error {
	parsedToken, err := uuid.Parse(token)
	if err != nil {
		return fmt.Errorf("parse turn token: %w", err)
	}
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	affected, err := s.q.RenewConversationTurn(ctx, pgstore.RenewConversationTurnParams{
		LeaseSeconds: leaseSeconds, ID: id, WorkspaceID: workspaceID, TurnToken: parsedToken,
	})
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrTurnLost
	}
	return nil
}

// CommitTurn appends messages and clears the lease in one transaction.
func (s *PostgresStore) CommitTurn(ctx context.Context, workspaceID, conversationID, token string, messages []*domain.Message, status string, userPromptSnapshot []byte) error {
	parsedToken, err := uuid.Parse(token)
	if err != nil {
		return fmt.Errorf("parse turn token: %w", err)
	}
	cid, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin conversation commit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if len(userPromptSnapshot) != 0 {
		var snapshot domain.UserPromptSnapshot
		if err := json.Unmarshal(userPromptSnapshot, &snapshot); err != nil {
			return fmt.Errorf("decode conversation prompt snapshot: %w", err)
		}
		if snapshot.VersionID != "" {
			id, err := uuid.Parse(snapshot.VersionID)
			if err != nil {
				return fmt.Errorf("parse conversation prompt version: %w", err)
			}
			if err := prompt.LockVersionForReference(ctx, q, workspaceID, id); err != nil {
				return fmt.Errorf("lock conversation prompt version: %w", err)
			}
		}
	}
	base := time.Now().UTC()
	for i, message := range messages {
		mid, err := uuid.Parse(message.ID)
		if err != nil {
			return fmt.Errorf("parse message id: %w", err)
		}
		callList := message.ToolCalls
		if callList == nil {
			callList = []domain.ToolCall{}
		}
		calls, err := json.Marshal(callList)
		if err != nil {
			return fmt.Errorf("encode message tool calls: %w", err)
		}
		var toolCallID pgtype.Text
		if message.ToolCallID != nil {
			toolCallID = pgtype.Text{String: *message.ToolCallID, Valid: true}
		}
		createdAt := base.Add(time.Duration(i) * time.Millisecond)
		if !message.CreatedAt.IsZero() {
			createdAt = message.CreatedAt
		}
		if _, err := q.InsertMessage(ctx, pgstore.InsertMessageParams{
			ID: mid, ConversationID: cid, Role: message.Role, Content: message.Content,
			ToolCalls: calls, ToolCallID: toolCallID, CreatedAt: createdAt,
		}); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		for _, call := range message.ToolCalls {
			callRow, err := uuid.Parse(call.ID)
			if err != nil {
				return fmt.Errorf("parse tool call id: %w", err)
			}
			args := domain.ToolArgumentsObject(call.Args)
			var result []byte
			if call.Result != "" {
				result = []byte(call.Result)
				if !json.Valid(result) {
					// 普通文本也可以写入 JSONB，先编码成 JSON 字符串。
					result, err = json.Marshal(call.Result)
					if err != nil {
						return fmt.Errorf("encode tool result: %w", err)
					}
				}
			}
			if _, err := q.InsertToolCall(ctx, pgstore.InsertToolCallParams{
				ID: callRow, MessageID: mid,
				ToolID: uuidValue(call.ToolID), ToolVersionID: uuidValue(call.ToolVersionID),
				Args: []byte(args), Result: result, LatencyMs: int32(call.LatencyMs), Error: call.Error,
			}); err != nil {
				return fmt.Errorf("insert tool call: %w", err)
			}
		}
	}
	affected, err := q.FinishConversationTurn(ctx, pgstore.FinishConversationTurnParams{
		Status: orStatus(status), ID: cid, WorkspaceID: workspaceID, TurnToken: parsedToken,
		UserPromptSnapshot: userPromptSnapshot, RequireActiveLease: true,
	})
	if err != nil {
		return fmt.Errorf("finish conversation turn: %w", err)
	}
	if affected != 1 {
		return ErrTurnLost
	}
	return tx.Commit(ctx)
}

// 释放租约锁
func (s *PostgresStore) ReleaseTurn(ctx context.Context, workspaceID, conversationID, token, status string) error {
	parsedToken, err := uuid.Parse(token)
	if err != nil {
		return fmt.Errorf("parse turn token: %w", err)
	}
	cid, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	affected, err := s.q.FinishConversationTurn(ctx, pgstore.FinishConversationTurnParams{
		Status: orStatus(status), ID: cid, WorkspaceID: workspaceID, TurnToken: parsedToken,
	})
	if err != nil {
		return fmt.Errorf("release conversation turn: %w", err)
	}
	if affected != 1 {
		return ErrTurnLost
	}
	return nil
}

func uuidValue(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}
