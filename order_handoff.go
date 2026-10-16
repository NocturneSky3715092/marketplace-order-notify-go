package marketnotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type OrderUpdate struct {
	OrderID  string `json:"order_id"`
	SellerID string `json:"seller_id"`
	BuyerID  string `json:"buyer_id"`
	Kind     string `json:"kind"`
	AssetID  string `json:"asset_id,omitempty"`
	Note     string `json:"note,omitempty"`
}

type Notification struct {
	Channel   string
	Event     string
	AccountID string
	Data      map[string]string
}

func RouteOrderUpdate(in OrderUpdate) (Notification, error) {
	if in.OrderID == "" || in.SellerID == "" || in.BuyerID == "" {
		return Notification{}, errors.New("order_id, seller_id, and buyer_id are required")
	}

	data := map[string]string{"order_id": in.OrderID}
	switch in.Kind {
	case "seller_asset_ready":
		if in.AssetID == "" {
			return Notification{}, errors.New("asset_id is required for seller_asset_ready")
		}
		data["asset_id"] = in.AssetID
		return Notification{Channel: "buyer:" + in.BuyerID, Event: "asset.ready", AccountID: in.BuyerID, Data: data}, nil
	case "buyer_update":
		data["note"] = in.Note
		return Notification{Channel: "seller:" + in.SellerID, Event: "buyer.updated", AccountID: in.SellerID, Data: data}, nil
	case "order_handoff":
		data["asset_id"] = in.AssetID
		return Notification{Channel: "buyer:" + in.BuyerID, Event: "order.handoff", AccountID: in.BuyerID, Data: data}, nil
	default:
		return Notification{}, fmt.Errorf("unknown update kind %q", in.Kind)
	}
}

type Server struct {
	client *Client
}

func NewServer(client *Client) *Server {
	return &Server{client: client}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/update", s.handleOrderUpdate)
	mux.HandleFunc("POST /realtime/token", s.handleToken)
	return mux
}

func (s *Server) handleOrderUpdate(w http.ResponseWriter, r *http.Request) {
	var update OrderUpdate
	if err := decodeJSON(r, &update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	notification, err := RouteOrderUpdate(update)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	requestID := r.Header.Get("Idempotency-Key")
	if requestID == "" {
		requestID = "order:" + update.OrderID + ":" + update.Kind
	}
	err = s.client.Publish(r.Context(), PublishRequest{
		Channel: notification.Channel, Event: notification.Event,
		Data: notification.Data, AccountID: notification.AccountID,
	}, requestID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "published", "channel": notification.Channel, "event": notification.Event,
	})
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ClientID string `json:"client_id"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(r, &input); err != nil || input.ClientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_id is required"})
		return
	}
	if input.Role != "buyer" && input.Role != "seller" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "role must be buyer or seller"})
		return
	}
	channel := input.Role + ":" + input.ClientID
	data, err := s.client.IssueToken(r.Context(), TokenRequest{
		ClientID: input.ClientID, Channels: []string{channel},
		Capabilities: []string{"subscribe"}, TTLSeconds: 900,
	}, "token:"+input.Role+":"+input.ClientID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "notification delivery failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func ChannelFor(role, accountID string) (string, error) {
	role = strings.TrimSpace(role)
	accountID = strings.TrimSpace(accountID)
	if (role != "buyer" && role != "seller") || accountID == "" {
		return "", errors.New("role and account ID are required")
	}
	return role + ":" + accountID, nil
}

func ProvisionChannels(ctx context.Context, client *Client, sellerID, buyerID string) error {
	for _, item := range []struct{ role, id string }{{"seller", sellerID}, {"buyer", buyerID}} {
		channel, err := ChannelFor(item.role, item.id)
		if err != nil {
			return err
		}
		if err := client.CreateChannel(ctx, channel, "channel:"+channel); err != nil {
			return fmt.Errorf("create %s channel: %w", item.role, err)
		}
	}
	return nil
}
