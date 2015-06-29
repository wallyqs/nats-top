package toputils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/nats-io/gnatsd/server"
)

const (
	MAX_MONITORING_RETRIES = 5
	RETRY_WAIT             = 1 // second
)

// Takes a path and options, then returns a serialized connz, varz, or routez response
func Request(path string, opts map[string]interface{}) (interface{}, error) {
	var statz interface{}
	uri := fmt.Sprintf("http://%s:%d%s", opts["host"], opts["port"], path)

	switch path {
	case "/varz":
		statz = &server.Varz{}
	case "/connz":
		statz = &server.Connz{}
		uri += fmt.Sprintf("?limit=%d&s=%s", opts["conns"], opts["sort"])
	default:
		return nil, fmt.Errorf("invalid path '%s' for stats server", path)
	}

	successch := make(chan bool)
	failurech := make(chan error)

	go func() {
		var e error
		for retries := 0; ; retries++ {
			if retries >= 1 {
				// Backoff for a bit before polling again
				fmt.Printf("\033[1;1HCould not monitor gnatsd, backing off for %ds... (retries=%d)\n", RETRY_WAIT, retries)
				time.Sleep(RETRY_WAIT * time.Second)
			}

			if retries >= MAX_MONITORING_RETRIES {
				successch <- false
				failurech <- e
				break
			}

			resp, err := http.Get(uri)
			if err != nil {
				e = fmt.Errorf("could not get stats from server: %v\n", err)
				continue
			}

			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				e = fmt.Errorf("could not read response from upstream: %v\n", err)
				continue
			}

			err = json.Unmarshal(body, &statz)
			if err != nil {
				e = fmt.Errorf("could not unmarshal json: %v\n", err)
				continue
			}

			successch <- true
			break
		}
	}()

	success := <-successch
	if success {
		return statz, nil
	} else {
		err := <-failurech
		return nil, err
	}
}

// Takes a float and returns a human readable string
func Psize(s int64) string {
	size := float64(s)

	if size < 1024 {
		return fmt.Sprintf("%.0f", size)
	} else if size < (1024 * 1024) {
		return fmt.Sprintf("%.1fK", size/1024)
	} else if size < (1024 * 1024 * 1024) {
		return fmt.Sprintf("%.1fM", size/1024/1024)
	} else if size >= (1024 * 1024 * 1024) {
		return fmt.Sprintf("%.1fG", size/1024/1024/1024)
	} else {
		return "NA"
	}
}
