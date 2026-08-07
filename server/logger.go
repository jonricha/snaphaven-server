package main

import (
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type LogHub struct {
	mu        sync.RWMutex
	entries   []LogEntry
	listeners map[chan LogEntry]bool
	maxBuffer int
	logFile   *os.File
}

var globalLogHub *LogHub

func InitLogHub(logFilePath string) *LogHub {
	hub := &LogHub{
		entries:   make([]LogEntry, 0, 500),
		listeners: make(map[chan LogEntry]bool),
		maxBuffer: 500,
	}

	if logFilePath != "" {
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			hub.logFile = f
		}
	}

	log.SetFlags(0)
	mw := io.MultiWriter(os.Stdout, hub)
	log.SetOutput(mw)
	globalLogHub = hub
	return hub
}

func LogEvent(msg string) {
	if globalLogHub != nil {
		globalLogHub.Write([]byte(msg + "\n"))
	} else {
		log.Println(msg)
	}
}

func (h *LogHub) Write(p []byte) (n int, err error) {
	h.mu.Lock()
	msg := string(p)
	entry := LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Message:   msg,
	}

	h.entries = append(h.entries, entry)
	if len(h.entries) > h.maxBuffer {
		h.entries = h.entries[1:]
	}

	if h.logFile != nil {
		h.logFile.WriteString(entry.Timestamp + " " + msg)
	}

	for ch := range h.listeners {
		select {
		case ch <- entry:
		default:
		}
	}
	h.mu.Unlock()

	return len(p), nil
}

func (h *LogHub) GetRecentLogs() []LogEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]LogEntry, len(h.entries))
	copy(result, h.entries)
	return result
}

func (h *LogHub) Subscribe() chan LogEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan LogEntry, 50)
	h.listeners[ch] = true
	return ch
}

func (h *LogHub) Unsubscribe(ch chan LogEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.listeners, ch)
	close(ch)
}
