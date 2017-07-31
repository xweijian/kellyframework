package argument_extrator

// request_decoder decodes a request using a specific serialization scheme.
type ArgumentExtractor interface {
	// Reads the request filling the RPC method argument.
	ExtractTo(args interface{}) error
}
