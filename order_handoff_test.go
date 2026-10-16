package marketnotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRouteOrderUpdate(t *testing.T) {
	tests := []struct {
		name        string
		input       OrderUpdate
		wantChannel string
		wantEvent   string
		wantErr     bool
	}{
		{
			name:        "seller asset reaches buyer",
			input:       OrderUpdate{OrderID: "ord-17", SellerID: "seller-4", BuyerID: "buyer-9", Kind: "seller_asset_ready", AssetID: "asset-2"},
			wantChannel: "buyer:buyer-9", wantEvent: "asset.ready",
		},
		{
			name:        "buyer note reaches seller",
			input:       OrderUpdate{OrderID: "ord-17", SellerID: "seller-4", BuyerID: "buyer-9", Kind: "buyer_update", Note: "approved"},
			wantChannel: "seller:seller-4", wantEvent: "buyer.updated",
		},
		{
			name:    "asset is required",
			input:   OrderUpdate{OrderID: "ord-17", SellerID: "seller-4", BuyerID: "buyer-9", Kind: "seller_asset_ready"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RouteOrderUpdate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RouteOrderUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Channel != tt.wantChannel || got.Event != tt.wantEvent {
				t.Fatalf("RouteOrderUpdate() = channel %q event %q", got.Channel, got.Event)
			}
		})
	}
}

func TestPublishRetriesAfterDecodedRateLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing bearer authentication")
		}
		if r.Header.Get("Idempotency-Key") != "order:ord-17:seller_asset_ready" {
			t.Fatal("missing stable idempotency key")
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]string{"code": "THROTTLED", "message": "retry later"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]string{"id": "msg-1"}})
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL
	client.http = server.Client()
	client.sleep = func(context.Context, time.Duration) error { return nil }
	err := client.Publish(context.Background(), PublishRequest{
		Channel: "buyer:buyer-9", Event: "asset.ready", Data: map[string]string{"order_id": "ord-17"}, AccountID: "buyer-9",
	}, "order:ord-17:seller_asset_ready")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}
