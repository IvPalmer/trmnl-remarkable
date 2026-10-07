package today

import (
	"context"
	"encoding/json"
)

type fakePost struct{ path, body string }

// fakeClient answers Get from canned JSON per path and Post through a func.
type fakeClient struct {
	get   map[string]string
	err   map[string]error
	post  func(path string, body any) (string, error)
	posts []fakePost
}

func (f *fakeClient) Get(_ context.Context, path string, out any) error {
	if err := f.err[path]; err != nil {
		return err
	}
	body, ok := f.get[path]
	if !ok {
		return &HTTPError{Status: 404, Message: "no route"}
	}
	return json.Unmarshal([]byte(body), out)
}

func (f *fakeClient) Post(_ context.Context, path string, body, out any) error {
	b, _ := json.Marshal(body)
	f.posts = append(f.posts, fakePost{path, string(b)})
	answer, err := f.post(path, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(answer), out)
}
