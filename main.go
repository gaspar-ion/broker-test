package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

var (
	sseClients = make(map[chan string]bool)
	sseMu      sync.Mutex
)

func main() {
	broker := flag.String("broker", "tcp://test.mosquitto.org:1883", "Broker URI. ex: tcp://10.10.1.1:1883")
	flag.Parse()

	id := uuid.New().String()
	opts := mqtt.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID(id + "_mqtt-sse-server")
	opts.SetAutoReconnect(true)

	topic := "#"

	// Cuando se establece la conexión (inicial o tras reconexión)
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		if token := client.Subscribe(topic, 0, nil); token.Wait() && token.Error() != nil {
			log.Println("Error al suscribirse:", token.Error())
		} else {
			log.Println("Suscripción exitosa al topic:", topic)
		}
	})

	// Cuando se pierde la conexión
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Println("Conexión perdida:", err)
	})

	// Manejador de mensajes recibidos
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		text := fmt.Sprintf("[%s] %s", msg.Topic(), string(msg.Payload()))
		broadcast(text)
	})

	client := mqtt.NewClient(opts)

	// Primer intento de conexión
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatal("Error conectando al broker MQTT:", token.Error())
	}
	client.Subscribe(topic, 0, nil)
	log.Println("Listening to", fmt.Sprintf("%s/%s", *broker, topic))

	// Página HTML
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, "index.html")
	})

	// Server-Sent Events
	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		msgChan := make(chan string)
		registerClient(msgChan)
		defer unregisterClient(msgChan)

		notify := w.(http.CloseNotifier).CloseNotify()

		for {
			select {
			case msg := <-msgChan:
				fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(msg, "\n", " "))
				w.(http.Flusher).Flush()
			case <-notify:
				return
			}
		}
	})

	fmt.Println("Servidor en http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}

// Manejo de clientes SSE

func registerClient(ch chan string) {
	sseMu.Lock()
	defer sseMu.Unlock()
	sseClients[ch] = true
}

func unregisterClient(ch chan string) {
	sseMu.Lock()
	defer sseMu.Unlock()
	delete(sseClients, ch)
	close(ch)
}

func broadcast(msg string) {
	sseMu.Lock()
	defer sseMu.Unlock()
	for ch := range sseClients {
		select {
		case ch <- msg:
		default:
			// cliente lento o desconectado
			delete(sseClients, ch)
			close(ch)
		}
	}
}
