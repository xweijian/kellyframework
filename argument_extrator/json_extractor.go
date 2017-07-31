package argument_extrator

import (
	"net/http"
	"encoding/json"
)

func NewJSONArgumentExtractor(r *http.Request) ArgumentExtractor {
	return &_JSONExtractor{r}
}

type _JSONExtractor struct {
	*http.Request
}

func (r *_JSONExtractor) ExtractTo(x interface{}) error {
	dec := json.NewDecoder(r.Request.Body)
	return dec.Decode(x)
}
