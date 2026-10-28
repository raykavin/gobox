// Package fcm sends push notifications through the Firebase Cloud Messaging
// HTTP v1 API, authenticated with a Google service account.
//
// # Sending a message
//
//	client, err := fcm.New(fcm.Config{Credentials: serviceAccountJSON})
//	if err != nil {
//	    return err
//	}
//
//	resp, err := client.Send(ctx, fcm.NotificationMessage{
//	    Address: registrationToken,
//	    Title:   "Order shipped",
//	    Body:    "Your order is on its way.",
//	    Link:    "/orders/42",
//	    Tag:     "order-42",
//	})
//
// # Errors and answers
//
// Send returns an error only when FCM was not reached: an empty address, a
// transport failure or a refused token exchange. Whenever FCM answers, the
// answer comes back as a SendResponse, accepted or not, and its Accepted,
// Retryable and InvalidToken methods tell the caller what to do next.
//
// Messages are data-only WebPush messages: Title, Body, Link and Tag travel
// in the data map, and the client's service worker renders the notification.
package fcm