package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
	sseClients = make(map[chan string]bool)
	sseMu      sync.Mutex
)

func main() {
	// MQTT
	broker := flag.String("broker", "tcp://test.mosquitto.org:1883", "Broker URI. ex: tcp://10.10.1.1:1883")
	flag.Parse()

	opts := mqtt.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID("mqtt-sse-server")
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		text := fmt.Sprintf("[%s] %s", msg.Topic(), string(msg.Payload()))
		broadcast(text)
	})

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	topic := "#"
	client.Subscribe(topic, 0, nil)
	log.Println("Listening to ", fmt.Sprintf("%s/%s", *broker, topic))

	// Página HTML
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `
<!DOCTYPE html>
<html>
<head><title>Eventos MQTT</title></head>
<body>
<h1>Eventos en tiempo real</h1>
<ul id="eventos"></ul>
<script>
const ul = document.getElementById("eventos");
const source = new EventSource("/events");
source.onmessage = function(e) {
	const li = document.createElement("li");
	li.textContent = e.data;
	ul.prepend(li);
};
</script>
</body>
</html>
		`)
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
