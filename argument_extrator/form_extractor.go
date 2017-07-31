package argument_extrator

import (
	"net/http"
	"github.com/gorilla/schema"
)

var formDecoder = schema.NewDecoder()

type formExtractor struct {
	*http.Request
}

func NewFormArgumentExtractor(r *http.Request) ArgumentExtractor {
	return &formExtractor{r}
}

func (r *formExtractor) ExtractTo(x interface{}) error {
	err := r.Request.ParseForm()
	if err != nil {
		return err
	}

	return formDecoder.Decode(x, r.Request.Form)
}
