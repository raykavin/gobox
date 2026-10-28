# fcm

The `fcm` package sends push notifications through the Firebase Cloud Messaging HTTP v1 API, authenticated with a Google service account. It is intended for backends that deliver WebPush notifications to browsers and want a small, dependency-light client instead of the full Firebase Admin SDK.

## Import

```go
import "github.com/raykavin/gobox/fcm"
```

## What it provides

- `New` for building a client from a service account key, validated up front without any network call
- `Client.Send` for delivering a data-only WebPush message to one registration token
- `SendResponse` with `Accepted`, `Retryable` and `InvalidToken`, so the caller can decide between done, retry later and discard the token without parsing FCM's error body
- Access tokens signed with the service account key (JWT bearer grant), cached and renewed shortly before they expire

The only third-party dependency is `golang.org/x/oauth2`.

## Main types

- `Config`: the project, the service account key and optional overrides for the endpoints and the HTTP client
- `Client`: the sender; safe for concurrent use, so build one per process and share it
- `NotificationMessage`: what is sent to one device: address, title, body, link, tag, extra data and TTL
- `SendResponse`: FCM's answer to a send, accepted or not
- `ErrorCode`: the reason FCM gives for a rejection

## Example

### Setup

```go
credentials, err := os.ReadFile("/run/secrets/firebase-service-account.json")
if err != nil {
    log.Fatal(err)
}

client, err := fcm.New(fcm.Config{
    Credentials: credentials,
})
if err != nil {
    log.Fatal(err)
}
```

`ProjectID` can be left empty: the service account's project is used. When set, it must match it.

### Sending and handling the answer

```go
resp, err := client.Send(ctx, fcm.NotificationMessage{
    Address: registrationToken,
    Title:   "Order shipped",
    Body:    "Your order is on its way.",
    Link:    "/orders/42",
    Tag:     "order-42",
    Data:    map[string]string{"delivery_id": deliveryID},
    TTL:     24 * time.Hour,
})
if err != nil {
    // FCM was not reached: transport failure, refused token exchange or an
    // empty address. Anything but ErrEmptyAddress is worth retrying.
    return err
}

switch {
case resp.Accepted():
    markDelivered(deliveryID, resp.AcceptedName)
case resp.InvalidToken():
    removeRegistration(registrationToken)
case resp.Retryable():
    scheduleRetry(deliveryID, resp.RetryAfter)
default:
    markFailed(deliveryID, resp.ErrorCode, resp.Body)
}
```

### Rendering on the client

Messages are data-only, so the service worker builds the notification from the data map:

```js
import { initializeApp } from "firebase/app";
import { getMessaging, onBackgroundMessage } from "firebase/messaging/sw";

const messaging = getMessaging(initializeApp(firebaseConfig));

onBackgroundMessage(messaging, ({ data }) => {
  self.registration.showNotification(data.title, {
    body: data.body,
    tag: data.tag,
    data: { link: data.link, deliveryId: data.delivery_id },
  });
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(clients.openWindow(event.notification.data.link || "/"));
});
```

## Reference

### Functions and methods

| Function                                                                       | Description                                                                                    |
| ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- |
| `New(cfg Config) (*Client, error)`                                             | Validates the key and the project and builds the client; makes no network call                |
| `(*Client) ProjectID() string`                                                 | The Firebase project messages are sent through                                                 |
| `(*Client) Send(ctx context.Context, msg NotificationMessage) (SendResponse, error)` | Sends one message; returns a `SendResponse` whenever FCM answers, an error otherwise      |
| `(SendResponse) Accepted() bool`                                               | FCM answered `200`                                                                             |
| `(SendResponse) Retryable() bool`                                              | The rejection is transient: `429`, any `5xx`, `QUOTA_EXCEEDED`, `UNAVAILABLE` or `INTERNAL`    |
| `(SendResponse) InvalidToken() bool`                                           | The token can no longer receive messages from this project: `UNREGISTERED` or `SENDER_ID_MISMATCH` |

### Config

| Field         | Default                               | Description                                                       |
| ------------- | ------------------------------------- | ----------------------------------------------------------------- |
| `ProjectID`   | the service account's `project_id`    | Firebase project messages are sent through                        |
| `Credentials` | required                              | Service account key file (JSON)                                   |
| `Endpoint`    | `DefaultEndpoint`                     | FCM API root; useful for tests                                    |
| `TokenURL`    | the key's `token_uri`, then `DefaultTokenURL` | OAuth 2.0 token endpoint                                  |
| `HTTPClient`  | a client with `DefaultTimeout` (15s)  | Used for both the token exchange and the sends; never modified    |

### NotificationMessage

| Field     | Description                                                                                         |
| --------- | --------------------------------------------------------------------------------------------------- |
| `Address` | FCM registration token of the device; surrounding spaces are trimmed                                |
| `Title`   | Cut to 512 bytes, sent as `data.title`                                                              |
| `Body`    | Cut to 2048 bytes, sent as `data.body`                                                              |
| `Link`    | Path inside the application opened on click, sent as `data.link`                                    |
| `Tag`     | Groups notifications on the device, sent as `data.tag`; a repeated tag replaces instead of stacking |
| `Data`    | Extra values for the client; `title`, `body`, `link` and `tag` are overwritten by the fields above  |
| `TTL`     | WebPush `TTL` header in whole seconds; zero lets FCM decide                                         |

### SendResponse

| Field          | Description                                                                          |
| -------------- | ------------------------------------------------------------------------------------ |
| `StatusCode`   | HTTP status FCM answered with                                                        |
| `Body`         | Raw answer, capped at 64 KB                                                          |
| `Headers`      | WebPush headers sent with the message (`Urgency`, `TTL`)                             |
| `AcceptedName` | Message name FCM assigned (`projects/*/messages/*`)                                  |
| `ErrorCode`    | FCM error code of a rejection, or the API status when the answer carries none        |
| `RetryAfter`   | Delay from the `Retry-After` header, in seconds or as an HTTP date; zero when absent |

### Errors

| Error                   | Returned when                                                                       |
| ----------------------- | ----------------------------------------------------------------------------------- |
| `ErrInvalidCredentials` | The key is not JSON, is not a `service_account` key, or its `private_key` is not RSA |
| `ErrMissingProject`     | Neither `Config.ProjectID` nor the key names a project                              |
| `ErrProjectMismatch`    | `Config.ProjectID` differs from the key's `project_id`                              |
| `ErrEmptyAddress`       | `Send` receives a message without a registration token                              |

Transport failures from `Send` are wrapped: `errors.Is` still matches `context.DeadlineExceeded` or `context.Canceled`, and `errors.As` still finds the `*oauth2.RetrieveError` of a refused token exchange.

## Notes

- `New` never calls Google. A key the token endpoint refuses (revoked, deleted, clock skew) surfaces as an `*oauth2.RetrieveError` from the first `Send`, so an outage at startup does not stop the service.
- A `200` is an acceptance even when the body carries no name. Treating it as a failure would make an at-least-once dispatcher send the message twice; `Tag` makes a duplicate show up once on the device anyway.
- `INVALID_ARGUMENT` is not reported by `InvalidToken`: FCM uses it for malformed tokens and malformed payloads alike. Inspect `Body` before discarding a token on it.
- Messages carry `Urgency: high`, so the browser wakes the device to deliver them.
- The data map counts every key and value against FCM's 4 KB limit. Title and body are cut on character boundaries, but keep `Data` small and never put secrets in it: it is readable by the client.
- The service account needs the `cloudmessaging.messages.create` permission, granted by the *Firebase Cloud Messaging API Admin* role.
- `fcm_test.go` runs against an `httptest` server standing in for both the token endpoint and FCM, with a key generated at test time.