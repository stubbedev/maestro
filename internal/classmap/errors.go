package classmap

// Exception is an exception thrown by the ported PHP code. Class is the
// fully qualified name of the PHP exception class (e.g. "RuntimeException",
// "Symfony\Component\Finder\Exception\DirectoryNotFoundException"), which
// Composer prints when it renders an uncaught exception.
type Exception struct {
	Class   string
	Message string
}

func (e *Exception) Error() string { return e.Message }

func runtimeException(message string) error {
	return &Exception{Class: "RuntimeException", Message: message}
}

func invalidArgumentException(message string) error {
	return &Exception{Class: "InvalidArgumentException", Message: message}
}
