package conversation

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultTurnLeaseSeconds  = 60
	defaultTurnRenewInterval = 30 * time.Second
	turnCommitTimeout        = 10 * time.Second
)

func (s *Service) renewTurn(
	ctx context.Context, cancelTurn context.CancelCauseFunc, workspaceID, conversationID, token string,
) func() {
	renewCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.turnRenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				checkCtx, cancelCheck := context.WithTimeout(renewCtx, 5*time.Second)
				err := s.store.RenewTurn(checkCtx, workspaceID, conversationID, token, s.turnLeaseSeconds)
				cancelCheck()
				if err != nil {
					if renewCtx.Err() == nil {
						cancelTurn(fmt.Errorf("%w: renew conversation turn: %w", ErrTurnLost, err))
					}
					return
				}
			}
		}
	}()
	return func() {
		stop()
		<-done
	}
}
