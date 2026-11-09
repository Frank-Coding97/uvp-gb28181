package resource

// A missing or unexpected database status must stay unknown rather than false.
func resourceOnline(status string) *bool {
	if status == "unknown" {
		return nil
	}
	online := status == "online"
	return &online
}
