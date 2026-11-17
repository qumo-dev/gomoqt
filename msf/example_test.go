package msf_test

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qumo-dev/gomoqt/moqt"
	"github.com/qumo-dev/gomoqt/msf"
)

// The bodies of these examples are also shown in docs/msf/*.mdx, in code
// blocks that start with "// Example: <name>". TestDocs_GoBlocksMatchExamples
// keeps the two identical, so the documented code compiles and runs.

func ExampleParseCatalog() {
	raw := []byte(`{
		"version": "draft-02",
		"generatedAt": 1746104606044,
		"tracks": [
			{"name": "video", "packaging": "loc", "isLive": true, "role": "video",
			 "codec": "av01.0.08M.10.0.110.09", "bitrate": 5000000, "framerate": 30,
			 "width": 1920, "height": 1080, "renderGroup": 1},
			{"name": "audio", "packaging": "loc", "isLive": true, "role": "audio",
			 "codec": "opus", "bitrate": 32000, "samplerate": 48000, "channelConfig": "2",
			 "renderGroup": 1}
		]
	}`)

	catalog, err := msf.ParseCatalog(raw)
	if err != nil {
		fmt.Println("invalid JSON or catalog shape:", err)
		return
	}
	if err := catalog.Validate(); err != nil {
		fmt.Println("violates MSF rules:", err)
		return
	}
	for _, track := range catalog.Tracks {
		fmt.Println(track.Name, track.Role, track.Codec)
	}
	// Output:
	// video video av01.0.08M.10.0.110.09
	// audio audio opus
}

func ExampleValidationError() {
	catalog := msf.Catalog{
		Version: msf.CurrentVersion,
		Tracks: []msf.Track{
			{Name: "video", Packaging: msf.PackagingLOC, Role: msf.RoleVideo, IsLive: new(true)},
		},
	}

	var validationErr *msf.ValidationError
	if errors.As(catalog.Validate(), &validationErr) {
		for _, problem := range validationErr.Problems {
			fmt.Println(problem)
		}
	}
	// Output:
	// tracks[0]: codec is required for loc tracks with role video
	// tracks[0]: bitrate is required for loc tracks with role video
	// tracks[0]: framerate is required for loc tracks with role video
}

func ExampleCatalog_ApplyDelta() {
	base := msf.Catalog{
		Version: msf.CurrentVersion,
		Tracks: []msf.Track{
			{Name: "video", Packaging: msf.PackagingLOC, IsLive: new(true), Bitrate: new(int64(5000000))},
			{Name: "old-audio", Packaging: msf.PackagingLOC, IsLive: new(true)},
		},
	}

	delta, err := msf.ParseCatalogDelta([]byte(`{
		"generatedAt": 1746104606044,
		"deltaUpdate": [
			{"op": "update", "tracks": [{"parentName": "video", "bitrate": 4000000}]},
			{"op": "add", "tracks": [{"name": "subtitle", "packaging": "loc", "isLive": true, "role": "subtitle"}]},
			{"op": "remove", "tracks": [{"name": "old-audio"}]}
		]
	}`))
	if err != nil {
		fmt.Println(err)
		return
	}

	updated, err := base.ApplyDelta(delta)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, track := range updated.Tracks {
		fmt.Println(track.Name)
	}
	fmt.Println("video bitrate:", *updated.Tracks[0].Bitrate)
	// Output:
	// video
	// subtitle
	// video bitrate: 4000000
}

func ExampleCatalog_ResolveVariables() {
	catalog := msf.Catalog{
		Version: msf.CurrentVersion,
		Tracks: []msf.Track{{
			Name:      "tracking-%id%",
			Namespace: "advertising/%event%",
			Packaging: msf.PackagingEventTimeline,
			EventType: "com.example.iab.vast",
			IsLive:    new(true),
		}},
	}

	vars := msf.ParseFragmentVariables("msf:live-demo--catalog&id=c3e5s7w&event=xyz")
	resolved, err := catalog.ResolveVariables(vars)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resolved.Tracks[0].Namespace, resolved.Tracks[0].Name)
	// Output:
	// advertising/xyz tracking-c3e5s7w
}

func ExampleNewBroadcast() {
	catalog := msf.Catalog{
		Version: msf.CurrentVersion,
		Tracks: []msf.Track{
			{
				Name: "video", Packaging: msf.PackagingLOC, Role: msf.RoleVideo, IsLive: new(true),
				Codec: "av01.0.08M.10.0.110.09", Bitrate: new(int64(5000000)), Framerate: new(int64(30)),
			},
			{
				Name: "audio", Packaging: msf.PackagingLOC, Role: msf.RoleAudio, IsLive: new(true),
				Codec: "opus", Bitrate: new(int64(32000)), SampleRate: new(int64(48000)), ChannelConfig: "2",
			},
		},
	}

	broadcast, err := msf.NewBroadcast(catalog)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(broadcast.CatalogTrackName(), len(broadcast.Catalog().Tracks))
	// Output:
	// catalog 2
}

func ExampleBroadcast_RegisterTrack() {
	broadcast, err := msf.NewBroadcast(msf.Catalog{Version: msf.CurrentVersion})
	if err != nil {
		fmt.Println(err)
		return
	}
	serveVideo := moqt.TrackHandlerFunc(func(tw *moqt.TrackWriter) { /* write groups to tw */ })

	// Register a track before any track that depends on it.
	err = broadcast.RegisterTrack(msf.Track{
		Name: "video", Packaging: msf.PackagingLOC, Role: msf.RoleVideo, IsLive: new(true),
		Codec: "av01.0.08M.10.0.110.09", Bitrate: new(int64(5000000)), Framerate: new(int64(30)),
	}, serveVideo)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = broadcast.RegisterTrack(msf.Track{
		Name: "timeline", Packaging: msf.PackagingMediaTimeline, IsLive: new(true),
		Depends: []msf.Dependency{msf.DependsOnTrack("video")},
	}, serveVideo)
	fmt.Println(err, len(broadcast.Catalog().Tracks))
	// Output:
	// <nil> 2
}

func ExampleBroadcast_RemoveTrack() {
	broadcast, err := msf.NewBroadcast(msf.Catalog{
		Version: msf.CurrentVersion,
		Tracks: []msf.Track{
			{Name: "video", Packaging: msf.PackagingLOC, IsLive: new(true)},
			{Name: "timeline", Packaging: msf.PackagingMediaTimeline, IsLive: new(true),
				Depends: []msf.Dependency{msf.DependsOnTrack("video")}},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	// "timeline" depends on "video", so removing "video" first is refused
	// and the catalog stays as it was.
	err = broadcast.RemoveTrack("video")
	var validationErr *msf.ValidationError
	fmt.Println(errors.As(err, &validationErr), len(broadcast.Catalog().Tracks))

	fmt.Println(broadcast.RemoveTrack("timeline"), broadcast.RemoveTrack("video"))
	fmt.Println(errors.Is(broadcast.RemoveTrack("video"), msf.ErrTrackNotFound))
	// Output:
	// true 2
	// <nil> <nil>
	// true
}

func ExampleEventTimelineRecord() {
	record := msf.EventTimelineRecord{
		Wallclock: new(int64(1756885678361)),
		Data:      json.RawMessage(`{"homeScore":2,"awayScore":0}`),
	}
	if err := record.Validate(); err != nil {
		fmt.Println(err)
		return
	}

	data, err := json.Marshal(record)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(data))
	// Output:
	// {"T":1756885678361,"data":{"homeScore":2,"awayScore":0}}
}

func ExampleTemplate_Entry() {
	template := msf.Template{
		DeltaMediaTime: 2002,
		DeltaLocation:  msf.Location{GroupID: 1},
		StartWallclock: 1759924158381,
		DeltaWallclock: 2002,
	}

	entry := template.Entry(3)
	fmt.Println(entry.MediaTime, entry.Location.GroupID, entry.Wallclock)
	// Output:
	// 6006 3 1759924164387
}
