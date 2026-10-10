package assign

// WithTransferRecorder is retained as a no-op source-compatibility option for
// internal callers. External OpenAPI grant revocation is no longer part of an
// assignment transaction.
func WithTransferRecorder(_ any) ServiceOption { return func(*Service) {} }
