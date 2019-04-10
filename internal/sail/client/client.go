package client

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/windmilleng/tilt/internal/hud/server"
	"github.com/windmilleng/tilt/internal/hud/webview"
	"github.com/windmilleng/tilt/internal/logger"
	"github.com/windmilleng/tilt/internal/store"
)

type SailAddress url.URL

func (a SailAddress) String() string {
	return (*url.URL)(&a).String()
}

type SailClient struct {
	addr SailAddress
	conn *websocket.Conn
	mu   sync.Mutex
}

func ProvideSailClient(addr SailAddress) *SailClient {
	return &SailClient{addr: addr}
}

func (s *SailClient) Teardown(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return
	}

	_ = s.conn.Close()
	s.conn = nil
}

func (s *SailClient) isConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

func (s *SailClient) broadcast(ctx context.Context, view webview.View) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return
	}

	err := s.conn.WriteJSON(view)
	if err != nil {
		logger.Get(ctx).Infof("broadcast(%s): %v", s.addr, err)
	}
}

func (s *SailClient) setConnection(ctx context.Context, conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn

	// set up socket control handling
	go func() {
		defer func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			_ = s.conn.Close()
			s.conn = nil
		}()

		for ctx.Err() != nil {
			// We need to read from the connection so that the websocket
			// library handles control messages, but we can otherwise discard them.
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()
}

func (s *SailClient) Connect(ctx context.Context) error {
	header := make(http.Header)
	header.Add("Origin", s.addr.Host)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, s.addr.String(), header)
	if err != nil {
		return err
	}
	s.setConnection(ctx, conn)
	return nil
}

func (s *SailClient) OnChange(ctx context.Context, st store.RStore) {
	if !s.isConnected() {
		return
	}

	state := st.RLockState()
	view := server.StateToWebView(state)
	st.RUnlockState()

	s.broadcast(ctx, view)
}
