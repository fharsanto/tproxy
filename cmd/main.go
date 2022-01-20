package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/mux"
)

var (
	hostTarget map[string]string
	hostProxy  map[string]*httputil.ReverseProxy = map[string]*httputil.ReverseProxy{}
)

type SResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    []string `json:"data,omitempty"`
}

type TResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type DebugTransport struct{}

func (DebugTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b, err := httputil.DumpRequestOut(r, false)
	if err != nil {
		return nil, err
	}
	fmt.Println(string(b))
	res, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	modifyResponse(res)
	return res, nil
}

// NewProxy takes target host and creates a reverse proxy
func NewProxy(targetHost string) (*httputil.ReverseProxy, error) {
	url, err := url.Parse(targetHost)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(url)

	// originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		// originalDirector(req)
		modifyRequest(req)
	}

	proxy.ModifyResponse = modifyResponse
	// proxy.ErrorHandler = errorHandler
	// proxy.Transport = DebugTransport{}
	return proxy, nil
}

func modifyRequest(r *http.Request) {
	mr := mux.CurrentRoute(r)
	mp, _ := mr.GetPathTemplate()
	// r.Header.Set("X-Proxy", "Simple-Reverse-Proxy")
	if target, ok := hostTarget[mp]; ok {
		ru, _ := url.Parse(target)
		r.URL.Scheme = ru.Scheme
		r.URL.Host = ru.Host
		r.URL.Path = ru.Path
		// r.URL.Path = strings.Replace(r.URL.Path, "/dev", "", 1)
		r.Host = ru.Host
		// r.Header.Set("HTTP_X_FORWARDED_FOR", ru.Host)
		log.Printf("host: %v, path: %v\n", r.URL.Host, r.URL.Path)
	}
}

/**
func errorHandler(w http.ResponseWriter, req *http.Request, err error) {
	// fmt.Printf("Got error while modifying response: %v \n", err)
	resp := SResponse{
		Success: false,
		Message: err.Error(),
	}
	jr, _ := json.Marshal(resp)
	// w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jr)
}

func getMessage(req interface{}, code int, b []byte) string {
	dm := http.StatusText(code)
	if m, ok := req.(map[string]interface{}); ok {
		if value, ok := m["message"]; ok {
			return fmt.Sprintf("%s", value)
		}
	}
	if req == nil && len(b) > 0 {
		return string(b)
	}
	return dm
}*/

func modifyResponse(r *http.Response) error {
	b, err := ioutil.ReadAll(r.Body) //Read content
	if err != nil {
		return err
	}
	err = r.Body.Close()
	if err != nil {
		return err
	}

	isGzip := strings.Contains(r.Header.Get("Content-Encoding"), "gzip")
	var v SResponse
	if err := json.Unmarshal(b, &v); err != nil {
		log.Printf("err parse json %v\n", err)
		if isGzip {
			log.Printf("parse encoding gzip ...\n")
			buf := bytes.NewBuffer(b)
			reader, errr := gzip.NewReader(buf)
			if errr != nil {
				log.Printf("err init gzip.Reader %v\n", errr)
			}
			// Use the stream interface to decode json from the io.Reader
			dec := json.NewDecoder(reader)
			err = dec.Decode(&v)
			if err != nil && err != io.EOF {
				log.Printf("err parse gzip json %v\n", err)
			}
			r.Header.Del("Content-Encoding")
		}
	}

	log.Printf("Real resp: %v \n", v)

	resp := &TResponse{
		Success: v.Success,
		Message: v.Message,
	}
	if len(v.Data) > 1 {
		m := make(map[string]string)
		for _, el := range v.Data {
			chunk := strings.Split(el, ":")
			if len(chunk) == 2 {
				m[chunk[0]] = chunk[1]
			}
		}
		resp.Data = m
	}

	if len(v.Data) == 1 {
		ss := []byte(v.Data[0])
		var i interface{}
		src := (*json.RawMessage)(&ss)
		err := json.Unmarshal(*src, &i)
		if err == nil {
			resp.Data = i
		}

		if err != nil {
			resp.Data = v.Data[0]
		}
	}

	jr, _ := json.Marshal(resp)
	r.Body = ioutil.NopCloser(bytes.NewBuffer(jr))
	r.ContentLength = int64(len(jr))
	r.Header.Set("Content-Length", fmt.Sprint(len(jr)))
	r.Header.Set("Content-Type", "application/json")
	// r.StatusCode = http.StatusOK
	log.Printf("Content-Type resp: %v\n", r.Header.Get("Content-Type"))
	log.Printf("Content-Encoding resp: %v\n", r.Header.Get("Content-Encoding"))
	log.Printf("resp: %v\n", resp)
	return nil
}

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mr := mux.CurrentRoute(r)
	mp, _ := mr.GetPathTemplate()
	log.Printf("url: %v, path: %v\n", r.URL.Path, mp)
	if target, ok := hostTarget[mp]; ok {
		remoteUrl, _ := url.Parse(target)
		host := remoteUrl.Scheme + "://" + remoteUrl.Host
		// host := target

		if fn, ok := hostProxy[host]; ok {
			fn.ServeHTTP(w, r)
			return
		}

		proxy, err := NewProxy(host)
		if err != nil {
			log.Fatalf("%v", err)
		}
		hostProxy[host] = proxy
		proxy.ServeHTTP(w, r)
		return
	}
	resp := TResponse{
		Success: false,
		Message: "Path or Method not allowed",
		Data:    nil,
	}
	jr, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jr)
}

func main() {
	if err := json.Unmarshal([]byte(os.Getenv("APP_URI_LIST")), &hostTarget); err != nil {
		log.Panicf("unable to load config URI %v", err)
	}
	r := mux.NewRouter()
	for k := range hostTarget {
		r.HandleFunc(k, ServeHTTP)
	}
	// Routes consist of a path and a handler function.
	// r.HandleFunc("/", ServeHTTP)
	// handle all requests to your server using the proxy
	// http.HandleFunc("/", ServeHTTP)
	log.Fatal(http.ListenAndServe(":8080", r))
}
