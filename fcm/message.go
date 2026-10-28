package fcm

import (
	"maps"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// maxTitleBytes and maxBodyBytes keep the data map comfortably inside FCM's
// 4 KB payload limit, which counts every key and value.
const (
	maxTitleBytes = 512
	maxBodyBytes  = 2048
)

// NotificationMessage is a notification addressed to one device.
type NotificationMessage struct {
	// Address is the device's FCM registration token.
	Address string
	// Title is cut to 512 bytes.
	Title string
	// Body is cut to 2048 bytes.
	Body string
	// Link is a path inside the application, opened when the notification
	// is clicked. The client resolves it against its own origin.
	Link string
	// Tag groups notifications on the device: a second message with the
	// same tag replaces the first instead of stacking, which is what makes
	// an at-least-once retry show up once.
	Tag string
	// Data is extra context the client can read, such as the delivery id it
	// reports receipts against. Values only, never secrets. The title, body,
	// link and tag keys are overwritten by the fields above.
	Data map[string]string
	// TTL is how long FCM keeps trying to reach an offline device, in whole
	// seconds. Zero lets FCM decide.
	TTL time.Duration
}

type sendRequest struct {
	Message message `json:"message"`
}

type message struct {
	Token   string            `json:"token"`
	Data    map[string]string `json:"data"`
	WebPush webPushConfig     `json:"webpush"`
}

type webPushConfig struct {
	Headers map[string]string `json:"headers"`
}

func newSendRequest(msg NotificationMessage) (sendRequest, error) {
	token := strings.TrimSpace(msg.Address)
	if token == "" {
		return sendRequest{}, ErrEmptyAddress
	}

	data := make(map[string]string, len(msg.Data)+4)
	maps.Copy(data, msg.Data)
	data["title"] = truncateBytes(msg.Title, maxTitleBytes)
	data["body"] = truncateBytes(msg.Body, maxBodyBytes)
	data["link"] = msg.Link
	data["tag"] = msg.Tag

	headers := map[string]string{"Urgency": "high"}
	if msg.TTL > 0 {
		headers["TTL"] = strconv.FormatInt(int64(msg.TTL/time.Second), 10)
	}

	return sendRequest{
		Message: message{
			Token:   token,
			Data:    data,
			WebPush: webPushConfig{Headers: headers},
		},
	}, nil
}

// truncateBytes cuts s to at most limit bytes without splitting a character.
func truncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}
