package moqt

type SubscribeDrop struct {
	StartGroup GroupSequence
	EndGroup   GroupSequence
	ErrorCode  SubscribeErrorCode
}
