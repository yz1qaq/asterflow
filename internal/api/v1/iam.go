package v1

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"golang.org/x/crypto/bcrypt"
)

type IAMHandler struct {
	service *iam.Service
}

type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func NewIAMHandler(service *iam.Service) *IAMHandler {
	return &IAMHandler{service: service}
}

func (h *IAMHandler) Login(c *gin.Context) {
	var req iam.LoginRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	response, err := h.service.Login(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, iam.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid email or password",
			})
		case errors.Is(err, iam.ErrAccountInactive):
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "user account is not active",
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "login is unavailable",
			})
		}
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *IAMHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := decodeJSON(c.Writer, c.Request, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	user, err := h.service.CreateUser(
		c.Request.Context(),
		req.Email,
		req.Password,
		req.Name,
	)
	if err != nil {
		switch {
		case errors.Is(err, iam.ErrInvalidEmail),
			errors.Is(err, iam.ErrWeakPassword),
			errors.Is(err, iam.ErrNameRequired),
			errors.Is(err, bcrypt.ErrPasswordTooLong):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, iam.ErrEmailExists):
			c.JSON(http.StatusConflict, gin.H{"error": "email already exists"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "create user failed",
			})
		}
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *IAMHandler) ListUsers(c *gin.Context) {
	limit, offset, err := pageParams(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	users, err := h.service.ListUsers(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "list users failed",
		})
		return
	}
	c.JSON(http.StatusOK, users)
}
