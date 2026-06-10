package message_test

import (
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func BenchmarkSubscribeMessage_Encode(b *testing.B) {
	msg := message.SubscribeMessage{
		SubscribeID:        12345,
		BroadcastPath:      "this/is/a/long/broadcast/path/to/test/performance",
		TrackName:          "high-quality-video-track",
		SubscriberPriority: 100,
		SubscriberOrdered:  1,
		SubscriberMaxLatency: 500,
		StartGroup:         10,
		EndGroup:           20,
	}

	w := discardWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		msg.Encode(w)
	}
}
