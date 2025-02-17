package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for demo
	},
}

type WebsocketMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type Client struct {
	conn           *websocket.Conn
	peerConnection *webrtc.PeerConnection
	mu            sync.Mutex
}

func main() {
	// WebRTC configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	// Serve the client webpage
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// WebSocket endpoint
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Failed to upgrade connection: %v", err)
			return
		}

		client := &Client{
			conn: conn,
		}

		// Create PeerConnection
		peerConnection, err := webrtc.NewPeerConnection(config)
		if err != nil {
			log.Printf("Failed to create peer connection: %v", err)
			return
		}
		client.peerConnection = peerConnection

		// Set up ICE candidate handling
		peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
				return
			}

			candidateJSON, err := json.Marshal(candidate.ToJSON())
			if err != nil {
				log.Printf("Failed to marshal candidate: %v", err)
				return
			}

			message := WebsocketMessage{
				Event: "candidate",
				Data:  candidateJSON,
			}

			client.mu.Lock()
			defer client.mu.Unlock()
			if err := client.conn.WriteJSON(message); err != nil {
				log.Printf("Failed to send candidate: %v", err)
			}
		})

		// Handle incoming tracks
		peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			fmt.Printf("Received track of kind: %s\n", track.Kind())
		})

		// WebSocket message handling loop
		for {
			var msg WebsocketMessage
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Printf("Error reading message: %v", err)
				break
			}

			switch msg.Event {
			case "offer":
				var offer webrtc.SessionDescription
				if err := json.Unmarshal(msg.Data, &offer); err != nil {
					log.Printf("Failed to parse offer: %v", err)
					continue
				}

				if err := peerConnection.SetRemoteDescription(offer); err != nil {
					log.Printf("Failed to set remote description: %v", err)
					continue
				}

				// Create answer
				answer, err := peerConnection.CreateAnswer(nil)
				if err != nil {
					log.Printf("Failed to create answer: %v", err)
					continue
				}

				if err := peerConnection.SetLocalDescription(answer); err != nil {
					log.Printf("Failed to set local description: %v", err)
					continue
				}

				answerJSON, err := json.Marshal(answer)
				if err != nil {
					log.Printf("Failed to marshal answer: %v", err)
					continue
				}

				response := WebsocketMessage{
					Event: "answer",
					Data:  answerJSON,
				}

				client.mu.Lock()
				if err := client.conn.WriteJSON(response); err != nil {
					log.Printf("Failed to send answer: %v", err)
				}
				client.mu.Unlock()

			case "candidate":
				var candidate webrtc.ICECandidateInit
				if err := json.Unmarshal(msg.Data, &candidate); err != nil {
					log.Printf("Failed to parse candidate: %v", err)
					continue
				}

				if err := peerConnection.AddICECandidate(candidate); err != nil {
					log.Printf("Failed to add ICE candidate: %v", err)
					continue
				}
			}
		}

		// Clean up
		if err := peerConnection.Close(); err != nil {
			log.Printf("Failed to close peer connection: %v", err)
		}
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close websocket: %v", err)
		}
	})

	fmt.Println("Server running on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}