package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClient = http.Client{
	Timeout: 5 * time.Second,
}

//nolint:gosec
func getSolutionPage(uuid string, params map[string]string) (content string, urlv string, err error) {
	// form URL
	urlv = strings.Join([]string{exercismAddr, "tracks", trackLang, "exercises", exercise, "solutions", uuid}, "/")
	// form params
	if len(params) > 0 {
		vs := url.Values{}
		for k, v := range params {
			vs.Add(k, v)
		}
		urlv += "?" + vs.Encode()
	}
	// create request
	req, err := http.NewRequest("GET", urlv, nil)
	if err != nil {
		return
	}

	// do request
	resp, err := httpClient.Do(req)
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
	return string(bs), urlv, nil
}
