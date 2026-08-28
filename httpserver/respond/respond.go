package respond

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response is the standard API response envelope returned by all handlers.
type Response struct {
	Success bool       `json:"success"`
	Message string     `json:"message,omitempty"`
	Data    any        `json:"data,omitempty"`
	Errors  []APIError `json:"errors,omitempty"`
} // @name respond.Response

// APIError contains detailed information about a single error.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
} // @name respond.APIError

// Default messages for common responses.
const (
	msgOK                 = "The request was processed successfully"
	msgAccepted           = "The request was accepted for processing"
	msgCreated            = "The resource was created successfully"
	msgBadRequest         = "Invalid request body"
	msgUnauthorized       = "Unauthorized access"
	msgForbidden          = "Access denied, you do not have permission to access this resource"
	msgNotFound           = "Resource not found"
	msgConflict           = "Conflict with the current state of the resource"
	msgUnprocessable      = "Unprocessable entity"
	msgTooManyRequest     = "Too many requests"
	msgInternalError      = "An internal server error occurred"
	msgServiceUnavailable = "The service is currently unavailable"
)

// OK sends a 200 OK response with an optional custom message.
func OK(ctx *gin.Context, data any, message ...string) {
	ctx.JSON(http.StatusOK, successResponse(data, pickMessage(msgOK, message...)))
}

// Accepted sends a 202 Accepted response with an optional custom message.
func Accepted(ctx *gin.Context, data any, message ...string) {
	ctx.JSON(http.StatusAccepted, successResponse(data, pickMessage(msgAccepted, message...)))
}

// Created sends a 201 Created response with an optional custom message.
func Created(ctx *gin.Context, data any, message ...string) {
	ctx.JSON(http.StatusCreated, successResponse(data, pickMessage(msgCreated, message...)))
}

// NoContent sends a 204 No Content respond. No body is written.
func NoContent(ctx *gin.Context) {
	ctx.Status(http.StatusNoContent)
}

// BadRequest sends a 400 Bad Request respond.
func BadRequest(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusBadRequest, errorResponse(errs, msgBadRequest))
}

// Unauthorized sends a 401 Unauthorized respond.
func Unauthorized(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse(errs, msgUnauthorized))
}

// Forbidden sends a 403 Forbidden respond.
func Forbidden(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusForbidden, errorResponse(errs, msgForbidden))
}

// NotFound sends a 404 Not Found respond.
func NotFound(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusNotFound, errorResponse(errs, msgNotFound))
}

// Conflict sends a 409 Conflict respond.
func Conflict(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusConflict, errorResponse(errs, msgConflict))
}

// UnprocessableEntity sends a 422 Unprocessable Entity respond.
// Useful for validation errors after the request has been parsed successfully.
func UnprocessableEntity(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusUnprocessableEntity, errorResponse(errs, msgUnprocessable))
}

// TooManyRequests sends a 429 Too Many Requests respond.
func TooManyRequests(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusTooManyRequests, errorResponse(errs, msgTooManyRequest))
}

// InternalServerError sends a 500 Internal Server Error respond.
func InternalServerError(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusInternalServerError, errorResponse(errs, msgInternalError))
}

// ServiceUnavailable sends a 503 Server Unavailable Error respond.
func ServiceUnavailable(ctx *gin.Context, errs ...APIError) {
	ctx.AbortWithStatusJSON(http.StatusServiceUnavailable, errorResponse(errs, msgServiceUnavailable))
}

// Error sends a custom error response with the given HTTP status code.
// Use this when none of the specific helpers above fit.
func Error(ctx *gin.Context, status int, errs ...APIError) {
	ctx.AbortWithStatusJSON(status, errorResponse(errs, msgInternalError))
}

// NewError is a convenience constructor for building an APIError.
func NewError(code, message string, details ...any) APIError {
	e := APIError{Code: code, Message: message}
	if len(details) > 0 {
		e.Details = details[0]
	}
	return e
}

func pickMessage(def string, msg ...string) string {
	if len(msg) > 0 && msg[0] != "" {
		return msg[0]
	}
	return def
}

func successResponse(data any, message string) *Response {
	return &Response{
		Success: true,
		Message: message,
		Data:    data,
	}
}

func errorResponse(errs []APIError, message string) *Response {
	return &Response{
		Success: false,
		Message: message,
		Errors:  errs,
	}
}
