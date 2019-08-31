package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

//nolint:gosec
func getContent(path string) (content string, url string, err error) {
	url = strings.Join([]string{exercismAddr, "tracks", trackLang, "exercises", exerciseFlag, path}, "/")

	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("status code %q", resp.Status)
		return
	}

	bs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	return string(bs), url, nil
}
