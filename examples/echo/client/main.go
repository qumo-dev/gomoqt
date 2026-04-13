package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/okdaichi/gomoqt/moqt"
)

func main() {
	moqt.PublishFunc(context.Background(), "/client.echo", func(tw *moqt.TrackWriter) {
		frame := moqt.NewFrame(1024)
		for {
			time.Sleep(100 * time.Millisecond)

			gw, err := tw.OpenGroup()
			if err != nil {
				slog.Error("failed to open group", "error", err)
				return
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
