---
linkTitle: Track/Group/Frame
title: Track, Group, and Frame
weight: 4
---

Media streams are organized into a hierarchy of tracks, groups, and frames.

- [**Track**](#track): A continuous media stream, such as video or audio, within a broadcast path.
- [**Group**](#group): A self-contained segment of a track, often corresponding to a time-based unit like a video GOP or an audio packet group.
- [**Frame**](#frame): The smallest unit of media data, such as a single video image or audio sample.

## Track

Each track has a name that distinguishes it within the broadcast path, often corresponding to a specific media layer (e.g., SVC).

### `moqt.TrackWriter`

The `moqt.TrackWriter` handles all aspects of writing data to a track, including managing the lifecycle of the track, opening and closing groups, transmitting media data, and dealing with errors.

```go
type TrackWriter struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName
	// contains filtered or unexported fields
}

func (*TrackWriter) Close() error
func (*TrackWriter) CloseWithError(SubscribeErrorCode)
func (*TrackWriter) OpenGroup(context.Context) (*GroupWriter, error)
func (*TrackWriter) OpenGroupAt(context.Context, GroupSequence) (*GroupWriter, error)
func (*TrackWriter) SkipGroups(n uint64)
func (*TrackWriter) DropGroups(SubscribeDrop) error
func (*TrackWriter) DropNextGroups(n uint64, code SubscribeErrorCode) error
func (*TrackWriter) WriteInfo(PublishInfo) error
func (*TrackWriter) TrackConfig() *SubscribeConfig
func (*TrackWriter) ReadUpdate() (*SubscribeConfig, error)
func (*TrackWriter) Context() context.Context
```

### `moqt.TrackReader`

The `moqt.TrackReader` is responsible for reading data from a track, receiving groups and frames in order, and managing any errors that occur during the process.

```go
type TrackReader struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName
	// contains filtered or unexported fields
}

func (*TrackReader) AcceptGroup(context.Context) (*GroupReader, error)
func (*TrackReader) Close() error
func (*TrackReader) CloseWithError(SubscribeErrorCode)
func (*TrackReader) Update(*SubscribeConfig) error
func (*TrackReader) TrackConfig() *SubscribeConfig
func (*TrackReader) SubscribeID() SubscribeID
func (*TrackReader) Drops(context.Context) iter.Seq[SubscribeDrop]
func (*TrackReader) Context() context.Context
```

## Group

Groups are processed and transmitted independently, and may contain frames that are either standalone or interdependent (for example, I/P/B frames in video).

### `moqt.GroupWriter`
The GroupWriter writes frames, can set deadlines, cancel or close groups, and exposes the group's sequence.


```go
type GroupWriter struct {
    // contains filtered or unexported fields
}

func (*GroupWriter) GroupSequence() GroupSequence
func (*GroupWriter) WriteFrame(*Frame) error
func (*GroupWriter) SetWriteDeadline(time.Time) error
func (*GroupWriter) CancelWrite(GroupErrorCode)
func (*GroupWriter) Close() error
func (*GroupWriter) Context() context.Context
```

### `moqt.GroupReader`
The GroupReader reads frames sequentially, can cancel reads, and provides group sequencing and deadline control.

```go
type GroupReader struct {
	// contains filtered or unexported fields
}

func (*GroupReader) GroupSequence() GroupSequence
func (*GroupReader) ReadFrame(*Frame) error
func (*GroupReader) CancelRead(GroupErrorCode)
func (*GroupReader) SetReadDeadline(time.Time) error
func (*GroupReader) Frames(*Frame) iter.Seq[*Frame]
```

## Frame

Frames can be independent (like keyframes) or rely on other frames (like delta frames in video), and together they form the building blocks of a track.

### `moqt.Frame`

The Frame struct represents the smallest unit of media data. It ensures that data is stored immutably.

```go
type Frame struct {
	// contains filtered or unexported fields
}

func NewFrame(int) *Frame
func (*Frame) Body() []byte
func (*Frame) Cap() int
func (*Frame) Len() int
func (*Frame) Reset()
func (*Frame) Clone() *Frame
func (*Frame) Write([]byte) (int, error)
func (*Frame) WriteTo(w io.Writer) (int64, error)
```
