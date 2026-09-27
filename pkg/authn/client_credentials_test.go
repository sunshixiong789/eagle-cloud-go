package authn

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientCredentialsCachesToken(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"access_token":"service-token","expires_in":300}`)),
		}, nil
	})}

	credentials, err := NewClientCredentials(ClientCredentialsConfig{
		TokenURL: "http://keycloak/token", ClientID: "worker", ClientSecret: "secret", HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		token, err := credentials.Token(context.Background())
		if err != nil || token != "service-token" {
			t.Fatalf("Token() = %q, %v", token, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
}
