// Package api implements a client API for working with
// digitalrebar/provision.
package api

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/VictorLowther/jsonpatch2"

	"github.com/digitalrebar/logger"
	"github.com/digitalrebar/provision/models"
)

// APIPATH is the base path for all API endpoints that digitalrebar
// provision provides.
const APIPATH = "/api/v3"

// Client wraps *http.Client to include our authentication routines
// and routines for handling some of the biolerplate CRUD operations
// against digitalrebar provision.
type Client struct {
	*http.Client
	mux                          *sync.Mutex
	endpoint, username, password string
	token                        *models.UserToken
	closer                       chan struct{}
	closed                       bool
	traceLvl                     string
	traceToken                   string
}

func (c *Client) UrlFor(args ...string) (*url.URL, error) {
	return url.ParseRequestURI(c.endpoint + path.Join(APIPATH, path.Join(args...)))
}

// Trace sets the log level that incoming requests generated by a
// Client will be logged at, overriding the levels they would normally
// be logged at on the server side.  Setting lvl to an empty string
// turns off tracing.
func (c *Client) Trace(lvl string) {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.traceLvl = lvl
}

// TraceToken is a unique token that the server-side logger will emit
// a log for at the Error log level. It can be used to tie logs
// generated on the server side to requests made by a specific Client.
func (c *Client) TraceToken(t string) {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.traceToken = t
}

// R encapsulates a single Request/Response round trip.  It has a slew
// of helper methods that can be chained together to handle all common
// operations with this API.  It handles capturing any errors that may
// occur in building and executing the request.
type R struct {
	c                    *Client
	method               string
	uri                  *url.URL
	header               http.Header
	body                 io.Reader
	Req                  *http.Request
	Resp                 *http.Response
	err                  *models.Error
	paranoid             bool
	traceLvl, traceToken string
}

// Req creates a new R for the current client.
// It defaults to using the GET method.
func (c *Client) Req() *R {
	c.mux.Lock()
	defer c.mux.Unlock()
	return &R{
		c:          c,
		traceLvl:   c.traceLvl,
		traceToken: c.traceToken,
		method:     "GET",
		header:     http.Header{},
		err: &models.Error{
			Type: "CLIENT_ERROR",
		},
	}
}

// Trace will arrange for the server to log this specific request at
// the passed-in Level, overriding any client Trace requests or the
// levels things would usually be logged at by the server.
func (r *R) Trace(lvl string) *R {
	r.traceLvl = lvl
	return r
}

// TraceToken is a unique token that the server-side logger will emit
// a log for at the Error log level. It can be used to tie logs
// generated on the server side to requests made by a specific Req.
func (r *R) TraceToken(t string) *R {
	r.traceToken = t
	return r
}

// Meth sets an arbitrary method for R
func (r *R) Meth(v string) *R {
	r.method = v
	return r
}

// Get sets the R method to GET
func (r *R) Get() *R {
	return r.Meth("GET")
}

func (r *R) List(prefix string) *R {
	return r.Get().UrlFor(prefix)
}

// Del sets the R method to DELETE
func (r *R) Del() *R {
	return r.Meth("DELETE")
}

// Head sets the R method to HEAD
func (r *R) Head() *R {
	return r.Meth("HEAD")
}

// Put sets the R method to PUT, and arranges for b to be used as the
// body of the request by calling r.Body(). If no body is desired, b
// can be nil
func (r *R) Put(b interface{}) *R {
	return r.Meth("PUT").Body(b)
}

// Patch sets the R method to PATCH, and arranges for b (which must be
// a valid JSON patch) to be used as the body of the request by
// calling r.Body().
func (r *R) Patch(b jsonpatch2.Patch) *R {
	return r.Meth("PATCH").Body(b)
}

// Must be used before PatchXXX calls
func (r *R) ParanoidPatch() *R {
	r.paranoid = true
	return r
}

func (r *R) PatchObj(old, new interface{}) *R {
	b, err := GenPatch(old, new, r.paranoid)
	if err != nil {
		r.err.AddError(err)
		return r
	}
	return r.Meth("PATCH").Body(b)
}

func (r *R) PatchTo(old, new models.Model) *R {
	if old.Prefix() != new.Prefix() || old.Key() != new.Key() {
		r.err.Model = old.Prefix()
		r.err.Model = old.Key()
		r.err.Errorf("Cannot patch from %T to %T, or change keys from %s to %s", old, new, old.Key(), new.Key())
		return r
	}
	return r.PatchObj(old, new).UrlForM(old)
}

func (r *R) Fill(m models.Model) error {
	r.err.Model = m.Prefix()
	r.err.Model = m.Key()
	if m.Key() == "" {
		r.err.Errorf("Cannot Fill %s with an empty key", m.Prefix())
		return r.err
	}
	return r.Get().UrlForM(m).Do(&m)
}

// Post sets the R method to POST, and arranged for b to be the body
// of the request by calling r.Body().
func (r *R) Post(b interface{}) *R {
	return r.Meth("POST").Body(b)
}

// Delete deletes a single object
func (r *R) Delete(m models.Model) error {
	r.err.Model = m.Prefix()
	r.err.Model = m.Key()
	if m.Key() == "" {
		r.err.Errorf("Cannot Delete %s with an empty key", m.Prefix())
		return r.err
	}
	return r.Del().UrlForM(m).Do(&m)
}

// UrlFor arranges for a sane request URL to be used for R.
// The generated URL will be in the form of:
//
//    /api/v3/path.Join(args...)
func (r *R) UrlFor(args ...string) *R {
	res, err := r.c.UrlFor(args...)
	if err != nil {
		r.err.AddError(err)
		return r
	}
	r.uri = res
	return r
}

// UrlForM is similar to UrlFor, but the prefix and key of the
// passed-in Model will be used as the first two path components in
// the URL after /api/v3.  If m.Key() == "", it will be omitted.
func (r *R) UrlForM(m models.Model, rest ...string) *R {
	r.err.Model = m.Prefix()
	r.err.Key = m.Key()
	args := []string{m.Prefix(), m.Key()}
	args = append(args, rest...)
	return r.UrlFor(args...)
}

// Params appends query parameters to the URL R will use.  r.UrlFor()
// or r.UrlForM() must have already been called.  You must pass an
// even number of parameters to Params
func (r *R) Params(args ...string) *R {
	if r.uri == nil {
		r.err.Errorf("Cannot call WithParams before UrlFor or UrlForM")
		return r
	}
	if len(args)&1 == 1 {
		r.err.Errorf("WithParams was not passed an even number of arguments")
		return r
	}
	values := url.Values{}
	for i := 1; i < len(args); i += 2 {
		values.Add(args[i-1], args[i])
	}
	r.uri.RawQuery = values.Encode()
	return r
}

// Filter is a helper for using freeform index operations.
// The prefix arg is the type of object you want to filter, and filterArgs
// describes how you want the results filtered.  Currently, filterArgs must be
// in the following format:
//
//    "reverse"
//        to reverse the order of the results
//    "sort" "indexName"
//        to sort the results according to indexName's native ordering
//    "limit" "number"
//        to limit the number of results returned
//    "offset" "number"
//        to skip <number> of results before returning
//    "indexName" "Eq/Lt/Lte/Gt/Gte/Ne" "value"
//        to return results Equal, Less Than, Less Than Or Equal, Greater Than, Greater Than Or Equal, or Not Equal to value according to IndexName
//    "indexName" "Between/Except" "lowerBound" "upperBound"
//        to return values Between(inclusive) lowerBound and Upperbound or its complement for Except.
//
// If formatArgs does not contain some valid combination of the above, the request will fail.
func (r *R) Filter(prefix string, filterArgs ...string) *R {
	r.Get().UrlFor(prefix)
	finalParams := []string{}
	i := 0
	for i < len(filterArgs) {
		filter := filterArgs[i]
		switch filter {
		case "reverse":
			finalParams = append(finalParams, filter, "true")
			i++
		case "sort", "limit", "offset":
			if len(filterArgs)-i < 2 {
				r.err.Errorf("Invalid Filter: %s requires exactly one parameter", filter)
				return r
			}
			finalParams = append(finalParams, filter, filterArgs[i+1])
			i += 2
		default:
			if len(filterArgs)-i < 2 {
				r.err.Errorf("Invalid Filter: %s requires an op and at least 1 parameter", filter)
				return r
			}
			op := strings.Title(strings.ToLower(filterArgs[i+1]))
			i += 2
			switch op {
			case "Eq", "Lt", "Lte", "Gt", "Gte", "Ne":
				if len(filterArgs)-i < 1 {
					r.err.Errorf("Invalid Filter: %s op %s requires 1 parameter", filter, op)
					return r
				}
				finalParams = append(finalParams, filter, fmt.Sprintf("%s(%s)", op, filterArgs[i]))
				i++
			case "Between", "Except":
				if len(filterArgs)-i < 2 {
					r.err.Errorf("Invalid Filter: %s op %s requires 2 parameters", filter, op)
					return r
				}
				finalParams = append(finalParams, filter, fmt.Sprintf("%s(%s,%s)", op, filterArgs[i], filterArgs[i+1]))
				i += 2
			default:
				r.err.Errorf("Invalid Filter %s: unknown op %s", filter, op)
				return r
			}
		}
	}
	return r.Params(finalParams...)
}

// Headers arranges for its arguments to be added as HTTP headers.
// You must pass an even number of arguments to Headers
func (r *R) Headers(args ...string) *R {
	if len(args)&1 == 1 {
		r.err.Errorf("WithHeaders was not passed an even number of arguments")
		return r
	}
	if r.header == nil {
		r.header = http.Header{}
	}
	for i := 1; i < len(args); i += 2 {
		r.header.Add(args[i-1], args[i])
	}
	return r
}

// Body arranges for b to be used as the body of the request.
// It also sets the Content-Type of the request depending on what the body is:
//
// If b is an io.Reader or a raw byte array, Content-Type will be set to application/octet-stream,
// otherwise Content-Type will be set to application/json.
//
// If b is something other than nil, an io.Reader, or a byte array,
// Body will attempt to marshal the object as a JSON byte array and
// use that.
func (r *R) Body(b interface{}) *R {
	switch obj := b.(type) {
	case nil:
		r.Headers("Content-Type", "application/json")
	case io.Reader:
		r.Headers("Content-Type", "application/octet-stream")
		r.body = obj
	case []byte:
		r.Headers("Content-Type", "application/octet-stream")
		r.body = bytes.NewBuffer(obj)
	default:
		r.Headers("Content-Type", "application/json")
		buf, err := json.Marshal(&obj)
		if err != nil {
			r.err.AddError(err)
		} else {
			r.body = bytes.NewBuffer(buf)
		}
	}
	return r
}

// Do attempts to execute the reqest built up by previous method calls
// on R.  If any errors occurred while building up the request, they
// will be returned and no API interaction will actually take place.
// Otherwise, Do will generate am http.Request, perform it, and
// marshal the results to val.  If any errors occur while processing
// the request, they will be reported in the returned error.
//
// If val is an io.Writer, the body of the request will be copied
// verbatim into val using io.Copy
//
// Otherwise, the response body will be unmarshalled into val as
// directed by the Content-Type header of the response.
func (r *R) Do(val interface{}) error {
	if r.uri == nil {
		r.err.Errorf("No URL to talk to")
		return r.err
	}
	if r.c.closed {
		r.err.Errorf("Connection Closed")
		return r.err
	}
	if r.err.ContainsError() {
		return r.err
	}
	if r.traceLvl != "" {
		r.Headers("X-Log-Request", r.traceLvl)
		r.Headers("X-Log-Token", r.traceToken)
	}
	r.Headers("Accept", "application/json")
	switch val.(type) {
	case io.Writer:
		r.Headers("Accept", "application/octet-stream")
	}
	req, err := http.NewRequest(r.method, r.uri.String(), r.body)
	if err != nil {
		r.err.AddError(err)
		return r.err
	}
	req.Header = r.header
	r.Req = req
	r.c.Authorize(req)
	resp, err := r.c.Do(req)
	if err != nil {
		r.err.AddError(err)
		return r.err
	}
	r.Resp = resp
	if resp != nil {
		defer resp.Body.Close()
	}
	if wr, ok := val.(io.Writer); ok && resp.StatusCode < 300 {
		_, err := io.Copy(wr, resp.Body)
		r.err.AddError(err)
		return r.err.HasError()
	}
	if r.method == "HEAD" {
		if resp.StatusCode <= 300 {
			return nil
		}
		r.err.Errorf(http.StatusText(resp.StatusCode))
		r.err.Code = resp.StatusCode
		return r.err
	}
	var dec Decoder
	ct := resp.Header.Get("Content-Type")
	mt, _, _ := mime.ParseMediaType(ct)
	switch mt {
	case "application/json":
		dec = json.NewDecoder(resp.Body)
	default:
		buf := &bytes.Buffer{}
		io.Copy(buf, resp.Body)
		log.Printf("Got %v: %v", ct, buf.String())
		log.Printf("%v", resp.Request.URL)
		log.Printf("%v", resp)
		r.err.Errorf("Cannot handle content-type %s", ct)
	}
	if dec == nil {
		r.err.Errorf("No decoder for content-type %s", ct)
		return r.err
	}
	if resp.StatusCode >= 400 {
		res := &models.Error{}
		if err := dec.Decode(res); err != nil {
			r.err.Code = resp.StatusCode
			r.err.AddError(err)
			return r.err
		}
		return res
	}
	if val != nil && resp.Body != nil && resp.ContentLength != 0 {
		r.err.AddError(dec.Decode(val))
	}
	if f, ok := val.(models.Filler); ok && err != nil {
		f.Fill()
	}
	return r.err.HasError()
}

// Close should be called whenever you no longer want to use this
// client connection.  It will stop any token refresh routines running
// in the background, and force any API calls made to this client that
// would communicate with the server to return an error
func (c *Client) Close() {
	c.closer <- struct{}{}
	close(c.closer)
	c.closed = true
}

// Token returns the current authentication token associated with the
// Client.
func (c *Client) Token() string {
	if c.token == nil {
		return ""
	}
	return c.token.Token
}

// Info returns some basic system information that was retrieved as
// part of the initial authentication.
func (c *Client) Info() (*models.Info, error) {
	res := &models.Info{}
	return res, c.Req().UrlFor("info").Do(res)
}

// Logs returns the currently buffered logs from the dr-provision server
func (c *Client) Logs() ([]logger.Line, error) {
	res := []logger.Line{}
	return res, c.Req().UrlFor("logs").Do(&res)
}

// Authorize sets the Authorization header in the Request with the
// current bearer token.  The rest of the helper methods call this, so
// you don't have to unless you are building your own http.Requests.
func (c *Client) Authorize(req *http.Request) error {
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+c.Token())
	}
	return nil
}

// ListBlobs lists the names of all the binary objects at 'at', using
// the indexing parameters suppied by params.
func (c *Client) ListBlobs(at string, params ...string) ([]string, error) {
	res := []string{}
	return res, c.Req().UrlFor(path.Join("/", at)).Params(params...).Do(&res)
}

// GetBlob fetches a binary blob from the server, writing it to the
// passed io.Writer.
func (c *Client) GetBlob(dest io.Writer, at ...string) error {
	return c.Req().UrlFor(path.Join("/", path.Join(at...))).Do(dest)
}

// PostBlob uploads the binary blob contained in the passed io.Reader
// to the location specified by at on the server.  You are responsible
// for closing the passed io.Reader.
func (c *Client) PostBlob(blob io.Reader, at ...string) (models.BlobInfo, error) {
	res := models.BlobInfo{}
	return res, c.Req().Post(blob).UrlFor(path.Join("/", path.Join(at...))).Do(&res)
}

// DeleteBlob deletes a blob on the server at the location indicated
// by 'at'
func (c *Client) DeleteBlob(at ...string) error {
	return c.Req().Del().UrlFor(path.Join("/", path.Join(at...))).Do(nil)
}

// AllIndexes returns all the static indexes available for all object
// types on the server.
func (c *Client) AllIndexes() (map[string]map[string]models.Index, error) {
	res := map[string]map[string]models.Index{}
	return res, c.Req().UrlFor("indexes").Do(res)
}

// Indexes returns all the static indexes available for a given type
// of object on the server.
func (c *Client) Indexes(prefix string) (map[string]models.Index, error) {
	res := map[string]models.Index{}
	return res, c.Req().UrlFor("indexes", prefix).Do(res)
}

// OneIndex tests to see if there is an index on the object type
// indicated by prefix for a specific parameter.  If the returned
// Index is empty, there is no such Index.
func (c *Client) OneIndex(prefix, param string) (models.Index, error) {
	res := models.Index{}
	return res, c.Req().UrlFor("indexes", prefix, param).Do(&res)
}

func (c *Client) ListModel(prefix string, params ...string) ([]models.Model, error) {
	ref, err := models.New(prefix)
	if err != nil {
		return nil, err
	}
	res := ref.SliceOf()
	err = c.Req().UrlForM(ref).Params(params...).Do(&res)
	if err != nil {
		return nil, err
	}
	return ref.ToModels(res), nil
}

// GetModel returns an object if type prefix with the unique
// identifier key, if such an object exists.  Key can be either the
// unique key for an object, or any field on an object that has an
// index that enforces uniqueness.
func (c *Client) GetModel(prefix, key string, params ...string) (models.Model, error) {
	res, err := models.New(prefix)
	if err != nil {
		return nil, err
	}
	return res, c.Req().UrlFor(res.Prefix(), key).Params(params...).Do(res)
}

func (c *Client) GetModelForPatch(prefix, key string, params ...string) (models.Model, models.Model, error) {
	ref, err := c.GetModel(prefix, key, params...)
	if err != nil {
		return nil, nil, err
	}
	return ref, models.Clone(ref), nil
}

// ExistsModel tests to see if an object exists on the server
// following the same rules as GetModel
func (c *Client) ExistsModel(prefix, key string) (bool, error) {
	err := c.Req().Head().UrlFor(prefix, key).Do(nil)
	if e, ok := err.(*models.Error); ok && e.Code == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

// FillModel fills the passed-in model with new information retrieved
// from the server.
func (c *Client) FillModel(ref models.Model, key string) error {
	err := c.Req().UrlFor(ref.Prefix(), key).Do(&ref)
	return err
}

// CreateModel takes the passed-in model and creates an instance of it
// on the server.  It will return an error if the passed-in model does
// not validate or if it already exists on the server.
func (c *Client) CreateModel(ref models.Model) error {
	err := c.Req().Post(ref).UrlFor(ref.Prefix()).Do(&ref)
	return err
}

// DeleteModel deletes the model matching the passed-in prefix and
// key.  It returns the object that was deleted.
func (c *Client) DeleteModel(prefix, key string) (models.Model, error) {
	res, err := models.New(prefix)
	if err != nil {
		return nil, err
	}
	return res, c.Req().Del().UrlFor(prefix, key).Do(&res)
}

func (c *Client) reauth(tok *models.UserToken) error {
	return c.Req().UrlFor("users", c.username, "token").Params("ttl", "600").Do(&tok)
}

// PatchModel attempts to update the object matching the passed prefix
// and key on the server side with the passed-in JSON patch (as
// sepcified in https://tools.ietf.org/html/rfc6902).  To ensure that
// conflicting changes are rejected, your patch should contain the
// appropriate test stanzas, which will allow the server to detect and
// reject conflicting changes from different sources.
func (c *Client) PatchModel(prefix, key string, patch jsonpatch2.Patch) (models.Model, error) {
	new, err := models.New(prefix)
	if err != nil {
		return nil, err
	}
	err = c.Req().Patch(patch).UrlFor(prefix, key).Do(&new)
	return new, err
}

func (c *Client) PatchTo(old models.Model, new models.Model) (models.Model, error) {
	return c.PatchToFull(old, new, false)
}

func (c *Client) PatchToFull(old models.Model, new models.Model, paranoid bool) (models.Model, error) {
	res := models.Clone(old)
	r := c.Req()
	if paranoid {
		r = r.ParanoidPatch()
	}
	err := r.PatchTo(old, new).Do(&res)
	if err != nil {
		return old, err
	}
	return res, err
}

// PutModel replaces the server-side object matching the passed-in
// object with the passed-in object.  Note that PutModel does not
// allow the server to detect and reject conflicting changes from
// multiple sources.
func (c *Client) PutModel(obj models.Model) error {
	return c.Req().Put(obj).UrlForM(obj).Do(&obj)
}

// TokenSession creates a new api.Client that will use the passed-in Token for authentication.
// It should be used whenever the API is not acting on behalf of a user.
func TokenSession(endpoint, token string) (*Client, error) {
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	c := &Client{
		mux:      &sync.Mutex{},
		endpoint: endpoint,
		Client:   &http.Client{Transport: tr},
		closer:   make(chan struct{}, 0),
		token:    &models.UserToken{Token: token},
	}
	go func() {
		<-c.closer
	}()
	return c, nil
}

// UserSession creates a new api.Client that can act on behalf of a
// user.  It will perform a single request using basic authentication
// to get a token that expires 600 seconds from the time the session
// is crated, and every 300 seconds it will refresh that token.
//
// UserSession does not currently attempt to cache tokens to
// persistent storage, although that may change in the future.
func UserSession(endpoint, username, password string) (*Client, error) {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	}
	c := &Client{
		mux:      &sync.Mutex{},
		endpoint: endpoint,
		username: username,
		password: password,
		Client:   &http.Client{Transport: tr},
		closer:   make(chan struct{}, 0),
	}
	basicAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	token := &models.UserToken{}
	if err := c.Req().
		UrlFor("users", c.username, "token").
		Headers("Authorization", "Basic "+basicAuth).
		Do(&token); err != nil {
		return nil, err
	}
	go func() {
		ticker := time.NewTicker(300 * time.Second)
		for {
			select {
			case <-c.closer:
				ticker.Stop()
				return
			case <-ticker.C:
				token := &models.UserToken{}
				if err := c.reauth(token); err != nil {
					log.Fatalf("Error reauthing token, aborting: %v", err)
				}
				c.mux.Lock()
				c.token = token
				c.mux.Unlock()
			}
		}
	}()
	c.token = token
	return c, nil
}
