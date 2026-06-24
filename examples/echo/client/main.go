package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/qumo-dev/gomoqt/moqt"
)

const (
	frameInterval = 100 * time.Millisecond
	openTimeout   = 100 * time.Millisecond
)

func main() {
	moqt.PublishFunc(context.Background(), "/client.echo", func(tw *moqt.TrackWriter) {
		frame := moqt.NewFrame(1024)
		ticker := time.NewTicker(frameInterval)
		defer ticker.Stop()

		for {
			select {
			case <-tw.Context().Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
				gw, err := tw.OpenGroup(ctx)
				cancel()
				if err != nil {
					slog.Error("failed to open group", "error", err)
					if tw.Context().Err() != nil {
						return
					}
					continue
				}

				frame.Reset()
				frame.Write([]byte("FRAME " + gw.GroupSequence().String()))

				err = gw.WriteFrame(frame)
				if err != nil {
					gw.CancelWrite(moqt.InternalGroupErrorCode)
					slog.Error("failed to write frame", "error", err)
					return
				}

				gw.Close()
			}
		}
	})

	client := moqt.Dialer{
		Logger: slog.Default(),
	}

	sess, err := client.Dial(context.Background(), "https://localhost:4444/echo", nil)
	if err != nil {
		slog.Error("failed to dial",
			"error", err,
		)
		return
	}

	ar, err := sess.AcceptAnnounce("/")
	if err != nil {
		slog.Error("failed to open announce stream",
			"error", err,
		)
		return
	}

	for {
		ann, err := ar.ReceiveAnnouncement(context.Background())
		if err != nil {
			slog.Error("failed to receive announcements",
				"error", err,
			)
			return
		}

		go func(ann *moqt.Announcement) {
			if !ann.IsActive() {
				return
			}

			tr, err := sess.Subscribe(context.Background(), ann.BroadcastPath(), "index", nil)
			if err != nil {
				slog.Error("failed to open track stream", "error", err)
				return
			}

			for {
				gr, err := tr.AcceptGroup(context.Background())
				if err != nil {
					slog.Error("failed to accept group", "error", err)
					return
				}

				go func(gr *moqt.GroupReader) {
					defer gr.CancelRead(moqt.InternalGroupErrorCode)
					frame := moqt.NewFrame(0)
					for {
						err := gr.ReadFrame(frame)
						if err != nil {
							if err == io.EOF {
								return
							}
							slog.Error("failed to read frame", "error", err)
							return
						}

						slog.Info("received a frame", "frame", string(frame.Body()))

						// TODO: Release the frame after reading
						// This is important to avoid memory leaks
					}
				}(gr)

			}
		}(ann)
	}
}
