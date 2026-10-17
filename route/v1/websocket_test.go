package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IceWhaleTech/CasaOS/service"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

type peerTestService struct {
	service.PeerService
	mu    sync.Mutex
	peers map[string]model2.PeerDriveDBModel
}

func (s *peerTestService) GetPeers() []model2.PeerDriveDBModel {
	s.mu.Lock()
	defer s.mu.Unlock()
	peers := []model2.PeerDriveDBModel{}
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}
	return peers
}
func (s *peerTestService) GetPeerByID(id string) model2.PeerDriveDBModel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peers[id]
}
func (s *peerTestService) GetPeerByName(name string) model2.PeerDriveDBModel {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, peer := range s.peers {
		if peer.DisplayName == name {
			return peer
		}
	}
	return model2.PeerDriveDBModel{}
}
func (s *peerTestService) CreatePeer(peer *model2.PeerDriveDBModel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[peer.ID] = *peer
}
func (s *peerTestService) DeletePeer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, id)
}

type peerTestRepository struct {
	service.Repository
	peers *peerTestService
}

func (r peerTestRepository) Peer() service.PeerService { return r.peers }

func TestWebSocketFailedUpgradeReturnsWithoutStartingPumps(t *testing.T) {
	w := httptest.NewRecorder()
	err := ConnectWebSocket(echo.New().NewContext(httptest.NewRequest("GET", "/v1/file/ws", nil), w))
	if err == nil || w.Code != http.StatusBadRequest {
		t.Fatal(w.Code, err)
	}
}

func TestConcurrentWebSocketsKeepIndependentConnections(t *testing.T) {
	previous := service.MyService
	service.MyService = peerTestRepository{peers: &peerTestService{peers: map[string]model2.PeerDriveDBModel{}}}
	defer func() { service.MyService = previous }()
	e := echo.New()
	e.GET("/ws", ConnectWebSocket)
	e.GET("/peers", GetPeers)
	server := httptest.NewServer(e)
	defer server.Close()
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			seen := map[string]bool{}
			for !seen["peers"] || !seen["ping"] || !seen["display-name"] {
				var message struct {
					Type string `json:"type"`
				}
				if err := conn.ReadJSON(&message); err != nil {
					t.Error(err)
					return
				}
				seen[message.Type] = true
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`)); err != nil {
				t.Error(err)
			}
			response, err := http.Get(server.URL + "/peers")
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			var data map[string]interface{}
			if err := json.NewDecoder(response.Body).Decode(&data); err != nil || response.StatusCode != 200 {
				t.Error(response.StatusCode, err)
			}
		}()
	}
	workers.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for len(handler.snapshot()) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("disconnected peers retained")
		}
		time.Sleep(time.Millisecond)
	}
}
