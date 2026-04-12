package moqt

// SubscribeID uniquely identifies a subscription within the session.
// It is used to correlate subscription-related messages such as groups and
// updates within a peer connection.
type SubscribeID uint64
