package storage

// SubmissionFilter filters submission queries
type SubmissionFilter struct {
	LicenseID *uint64
	Epoch     *uint64
	Limit     int
	Offset    int
}

// SubmissionItem for batch recording
type SubmissionItem struct {
	Epoch     uint64
	LicenseID uint64
	Y         []byte
	Proof     []byte
	Nonce     []byte
}
