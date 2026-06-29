// Package httpclient provides a thin HTTP client wrapper with helpers for
// common header presets, query parameter encoding, and response decompression.
//
// # Sending a request
//
//	body, status, err := httpclient.NewRequestWithContext(
//	    ctx,
//	    http.MethodGet,
//	    "https://api.example.com/users",
//	    map[string]string{"page": "1"},
//	    httpclient.DefaultJSONHeaders(),
//	    nil,
//	)
//
// # Decompression
//
// DecompressResponse wraps the response body in the correct reader based on
// the Content-Encoding header. Use it when the server returns a compressed
// response and you need a plain io.ReadCloser.
//
//	resp, err := http.DefaultClient.Do(req)
//	reader, err := httpclient.DecompressResponse(resp)
//	defer reader.Close()
//
// Supported encodings: gzip, deflate, br (Brotli), zstd, and identity.
package httpclient
