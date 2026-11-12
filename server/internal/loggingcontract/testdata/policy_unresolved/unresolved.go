package policyunresolved

type Logger interface {
	Info(string, ...any)
}

func Run(logger Logger, message string) {
	logger.Info(message)
}
