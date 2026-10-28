package fcm

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// ErrorCode is the reason FCM gives for rejecting a message.
type ErrorCode string

// Error codes FCM answers with. See
// https://firebase.google.com/docs/reference/fcm/rest/v1/ErrorCode.
const (
	ErrorUnspecified      ErrorCode = "UNSPECIFIED_ERROR"
	ErrorInvalidArgument  ErrorCode = "INVALID_ARGUMENT"
	ErrorUnregistered     ErrorCode = "UNREGISTERED"
	ErrorSenderIDMismatch ErrorCode = "SENDER_ID_MISMATCH"
	ErrorQuotaExceeded    ErrorCode = "QUOTA_EXCEEDED"
	ErrorUnavailable      ErrorCode = "UNAVAILABLE"
	ErrorInternal         ErrorCode = "INTERNAL"
	ErrorThirdPartyAuth   ErrorCode = "THIRD_PARTY_AUTH_ERROR"
)

// SendResponse is FCM's answer to a send, accepted or not.
type SendResponse struct {
	// StatusCode is the HTTP status FCM answered with.
	StatusCode int
	// Body is the raw answer, capped at 64 KB.
	Body []byte
	// Headers are the WebPush headers sent with the message.
	Headers map[string]string
	// AcceptedName is the message name FCM assigned
	// (projects/*/messages/*). It can be empty on an accepted message whose
	// answer carried none; a 200 is an acceptance regardless.
	AcceptedName string
	// ErrorCode is the reason for a rejection: the FCM error code, or the
	// API status when the answer carries none. Empty when accepted.
	ErrorCode ErrorCode
	// RetryAfter is the delay FCM asked for before a retry, zero when absent.
	RetryAfter time.Duration
}

// Accepted reports whether FCM accepted the message.
func (r SendResponse) Accepted() bool {
	return r.StatusCode == http.StatusOK
}

// Retryable reports whether the rejection is transient, so the same message
// can be sent again later.
func (r SendResponse) Retryable() bool {
	switch r.ErrorCode {
	case ErrorQuotaExceeded, ErrorUnavailable, ErrorInternal:
		return true
	}
	return r.StatusCode == http.StatusTooManyRequests || r.StatusCode >= http.StatusInternalServerError
}

// InvalidToken reports whether the registration token can no longer receive
// messages from this project and should be discarded.
func (r SendResponse) InvalidToken() bool {
	return r.ErrorCode == ErrorUnregistered || r.ErrorCode == ErrorSenderIDMismatch
}

func newSendResponse(resp *http.Response, body []byte, headers map[string]string) SendResponse {
	out := SendResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
		Headers:    headers,
	}

	if resp.StatusCode == http.StatusOK {
		var accepted struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(body, &accepted) == nil {
			out.AcceptedName = accepted.Name
		}
		return out
	}

	out.ErrorCode = parseErrorCode(body)
	out.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	return out
}

func parseErrorCode(body []byte) ErrorCode {
	var answer struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	for _, detail := range answer.Error.Details {
		if detail.ErrorCode != "" {
			return ErrorCode(detail.ErrorCode)
		}
	}
	return ErrorCode(answer.Error.Status)
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return max(time.Duration(seconds)*time.Second, 0)
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(time.Until(at), 0)
	}
	return 0
}
