package electrum

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Version flag for the library
const Version = "0.5.0"

// Protocol tags
const (
	Protocol10   = "1.0"
	Protocol11   = "1.1"
	Protocol12   = "1.2"
	Protocol14   = "1.4"
	Protocol14_2 = "1.4.2"

	BTCDecimals = 1e8
)

// Common errors
var (
	ErrDeprecatedMethod  = errors.New("DEPRECATED_METHOD")
	ErrUnavailableMethod = errors.New("UNAVAILABLE_METHOD")
	ErrRejectedTx        = errors.New("REJECTED_TRANSACTION")
	ErrUnreachableHost   = errors.New("UNREACHABLE_HOST")
)

// Message Delimiter, according to the protocol specification
// http://docs.electrum.org/en/latest/protocol.html#format
const (
	delimiter  = byte('\n')
	comma      = ","
	arrayStart = "["
	arrayEnd   = "]"

	DefaultBatchSize = 80
)

// Options define the available configuration options
type Options struct {
	// Address of the server to use for network communications
	Address string

	// Version advertised by the client instance
	Version string

	// Protocol version preferred by the client instance
	Protocol string

	// If set to true, will enable the client to continuously dispatch
	// a 'server.version' operation every 60 seconds
	KeepAlive bool

	// Agent identifier that will be transmitted to the server when required;
	// will be concatenated with the client version
	Agent string

	// If provided, will be used to setup a secure network connection with the server
	TLS *tls.Config

	// If provided, will be used as logging sink
	Log *log.Logger

	Timeout time.Duration
}

// Client defines the protocol client instance structure and interface
type Client struct {
	// Address of the remote server to use for communication
	Address string

	// Version of the client
	Version string

	// Protocol version preferred by the client instance
	Protocol string

	done         chan bool
	transport    *transport
	counter      int
	subs         map[int]*subscription
	ping         *time.Ticker
	log          *log.Logger
	agent        string
	bgProcessing context.Context
	cleanUp      context.CancelFunc
	resuming     context.Context
	stopResuming context.CancelFunc
	sync.Mutex

	txCache *TxCache
}

type subscription struct {
	method   string
	params   []any
	messages chan *response
	handler  func(*response)
	ctx      context.Context
}

// New will create and start processing on a new client instance
func New(options *Options) (*Client, error) {
	t, err := getTransport(&transportOptions{
		address: options.Address,
		tls:     options.TLS,
		timeout: options.Timeout,
	})
	if err != nil {
		return nil, err
	}

	// By default use the latest supported protocol version
	// https://electrumx.readthedocs.io/en/latest/protocol-changes.html
	if options.Protocol == "" {
		options.Protocol = Protocol14_2
	}

	// Use library version as default client version
	if options.Version == "" {
		options.Version = Version
	}

	// Use library identifier as default agent name
	if options.Agent == "" {
		options.Agent = "fairbank-electrum"
	}

	txCache, err := NewTxCache(nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		transport:    t,
		counter:      0,
		bgProcessing: ctx,
		cleanUp:      cancel,
		done:         make(chan bool),
		subs:         make(map[int]*subscription),
		log:          options.Log,
		agent:        fmt.Sprintf("%s-%s", options.Agent, options.Version),
		Address:      options.Address,
		Version:      options.Version,
		Protocol:     options.Protocol,
		txCache:      txCache,
	}

	// Automatically send a 'server.version' or 'server.ping' request every 60 seconds as a keep-alive
	// signal to the server
	if options.KeepAlive {
		client.ping = time.NewTicker(60 * time.Second)
		go func() {
			defer client.ping.Stop()
			for {
				select {
				case <-client.ping.C:
					// Deliberately ignore errors produced by "ping" messages
					// "server.ping" is not recognized by the server in the current release (1.4.3)
					if b, err := client.req("server.version", client.Version, client.Protocol).encode(); err == nil {
						/* #nosec */
						err = client.transport.sendMessage(b)
						if err != nil && client.log != nil {
							client.log.Println(err)
						}
					}
				case <-client.bgProcessing.Done():
					return
				}
			}
		}()
	}

	// Monitor transport state
	go func() {
		for {
			select {
			case s := <-client.transport.state:
				client.Lock()
				count := len(client.subs)
				client.Unlock()
				if s == Reconnected && count > 0 {
					go client.resumeSubscriptions()
				}
			case <-client.bgProcessing.Done():
				return
			}
		}
	}()

	go client.handleMessages()
	return client, nil
}

func (c *Client) debug(msg string, args ...any) {
	if c.log != nil {
		_ = c.log.Output(2, fmt.Sprintf(msg, args...))
	}
}

// Build a request object
func (c *Client) req(name string, params ...any) *request {
	c.Lock()
	defer c.Unlock()

	// If no parameters are specified send an empty array
	// http://docs.electrum.org/en/latest/protocol.html#request
	if len(params) == 0 {
		params = []any{}
	}
	req := &request{
		ID:     c.counter,
		Method: name,
		Params: params,
	}
	c.counter++
	return req
}

// Build a batch request object
func (c *Client) batchReq(name string, params [][]any) []*request {
	requests := make([]*request, len(params))

	for i, p := range params {
		requests[i] = c.req(name, p...)
	}

	return requests
}

// Receive incoming network messages and the 'stop' signal
func (c *Client) handleMessages() {
	for {
		select {
		case <-c.done:
			for id := range c.subs {
				c.removeSubscription(id)
			}
			c.cleanUp()
			return
		case err := <-c.transport.errors:
			c.debug("transport error: %s", err)
		case m := <-c.transport.messages:
			c.debug("received msg: %s", m)

			var result interface{}
			if err := json.Unmarshal(m, &result); err != nil {
				c.debug("error unmarshalling any: %v\n", err)
				break
			}

			var responses []*response
			if _, ok := result.([]interface{}); ok {
				// Batch response
				if err := json.Unmarshal(m, &responses); err != nil {
					c.debug("error unmarshalling batch responses: %v\n", err)
					break
				}
			} else {
				// Single response

				resp := &response{}
				if err := json.Unmarshal(m, resp); err != nil {
					c.debug("error unmarshalling one response: %v\n", err)
					break
				}

				responses = append(responses, resp)
			}

			for _, resp := range responses {
				c.handleResponse(resp)
			}
		}
	}
}

func (c *Client) handleResponse(resp *response) {
	// Message routed by method name
	if resp.Method != "" {
		c.Lock()
		for _, sub := range c.subs {
			if sub.method == resp.Method {
				sub.messages <- resp
			}
		}
		c.Unlock()

		return
	}

	// Message routed by ID
	c.Lock()
	sub, ok := c.subs[resp.ID]
	c.Unlock()
	if ok {
		sub.messages <- resp
	}
}

// Remove and existing messages subscription
func (c *Client) removeSubscription(id int) {
	c.Lock()
	defer c.Unlock()
	sub, ok := c.subs[id]
	if ok {
		close(sub.messages)
		delete(c.subs, id)
	}
}

// Restart processing of existing subscriptions; intended to be triggered after
// recovering from a dropped connection
func (c *Client) resumeSubscriptions() {
	c.Lock()
	defer c.Unlock()

	// Handle existing resume attempts
	if c.stopResuming != nil {
		c.stopResuming()
	}
	c.resuming, c.stopResuming = context.WithCancel(context.Background())

	// Wait for the connection to be responsive
	rt := time.NewTicker(2 * time.Second)
	defer rt.Stop()
WAIT:
	for {
		select {
		case <-rt.C:
			if _, err := c.ServerVersion(); err == nil {
				break WAIT
			}
		case <-c.resuming.Done():
			return
		case <-c.bgProcessing.Done():
			return
		}
	}

	// Restart existing subscriptions
	for id, sub := range c.subs {
		c.removeSubscription(id)
		sub.messages = make(chan *response)
		if err := c.startSubscription(sub); err != nil {
			c.debug("failed to resume subscription '%s' with error: %s\n", sub.method, err)
		}
	}
}

// Start a subscription processing loop
func (c *Client) startSubscription(sub *subscription) error {
	// Start processing loop
	// Will be terminating when closing the subscription's context or
	// by closing it's messages channel
	go func() {
		for {
			select {
			case msg, ok := <-sub.messages:
				if !ok {
					return
				}
				sub.handler(msg)
			case <-sub.ctx.Done():
				return
			}
		}
	}()

	// Register subscription
	req := c.req(sub.method, sub.params...)
	c.Lock()
	c.subs[req.ID] = sub
	c.Unlock()

	// Send request to the server
	b, err := req.encode()
	if err != nil {
		c.removeSubscription(req.ID)
		return err
	}
	if err := c.transport.sendMessage(b); err != nil {
		c.removeSubscription(req.ID)
		return err
	}
	return nil
}

// Dispatch a synchronous request, i.e. wait for it's result
func (c *Client) syncRequest(req *request) (*response, error) {
	// Setup a subscription for the request with proper cleanup
	res := make(chan *response)
	c.Lock()
	c.subs[req.ID] = &subscription{messages: res}
	c.Unlock()
	defer c.removeSubscription(req.ID)

	// Encode and dispatch the request
	b, err := req.encode()
	if err != nil {
		return nil, err
	}

	b = append(b, delimiter)

	// Log request
	c.debug("sending msg: %s", b)

	if err := c.transport.sendMessage(b); err != nil {
		return nil, err
	}

	// Wait for the response
	return <-res, nil
}

func encodeBatch(reqs []*request) ([]byte, error) {
	reqsJson := make([]string, len(reqs))
	for i, req := range reqs {
		x, err := req.encode()
		if err != nil {
			return nil, err
		}

		reqsJson[i] = string(x)
	}

	return []byte(arrayStart + strings.Join(reqsJson, comma) + arrayEnd), nil
}

// Dispatch a batch of synchronous requests, i.e. wait for it's result
func (c *Client) syncBatchRequest(reqs []*request) ([]*response, error) {
	reqMap := make(map[int]int, len(reqs))
	// Setup a subscription for the request with proper cleanup
	res := make(chan *response)
	c.Lock()
	for i, req := range reqs {
		c.subs[req.ID] = &subscription{messages: res}
		reqMap[req.ID] = i
	}
	c.Unlock()

	// Encode and dispatch the request
	b, err := encodeBatch(reqs)
	if err != nil {
		return nil, err
	}

	b = append(b, delimiter)

	// Log request
	c.debug("sending msg: %s", b)

	if err := c.transport.sendMessage(b); err != nil {
		return nil, err
	}

	// Wait for the response
	respCount := 0

	responses := make([]*response, len(reqs))
	for resp := range res {
		c.Lock()
		delete(c.subs, resp.ID)
		c.Unlock()

		responses[reqMap[resp.ID]] = resp

		respCount++

		if respCount == len(reqs) {
			close(res)
		}
	}

	return responses, nil
}

// Close will finish execution and properly terminate the underlying network transport
func (c *Client) Close() {
	c.transport.close()
	close(c.done)
}

// ServerPing will send a ping message to the server to ensure it is responding, and to keep the
// session alive. The server may disconnect clients that have sent no requests for roughly 10 minutes.
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#server-ping
func (c *Client) ServerPing() error {
	switch c.Protocol {
	case Protocol12:
		fallthrough
	case Protocol14:
		fallthrough
	case Protocol14_2:
		res, err := c.syncRequest(c.req("server.ping"))
		if err != nil {
			return err
		}
		if res.Error != nil {
			return errors.New(res.Error.Message)
		}
		return nil
	default:
		return ErrUnavailableMethod
	}
}

// ServerVersion will synchronously run a 'server.version' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#server-version
func (c *Client) ServerVersion() (*VersionInfo, error) {
	res, err := c.syncRequest(c.req("server.version", c.agent, c.Protocol))
	if err != nil {
		return nil, err
	}

	if res.Error != nil {
		return nil, errors.New(res.Error.Message)
	}

	info := &VersionInfo{}
	switch c.Protocol {
	case Protocol10:
		info.Software = res.Result.(string)
	case Protocol11:
		fallthrough
	case Protocol12:
		fallthrough
	case Protocol14:
		fallthrough
	case Protocol14_2:
		var d []string
		b, err := json.Marshal(res.Result)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
		info.Software = d[0]
		info.Protocol = d[1]
	}
	return info, nil
}

// ServerBanner will synchronously run a 'server.banner' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#server-banner
func (c *Client) ServerBanner() (string, error) {
	res, err := c.syncRequest(c.req("server.banner"))
	if err != nil {
		return "", err
	}

	if res.Error != nil {
		return "", errors.New(res.Error.Message)
	}

	return res.Result.(string), nil
}

// ServerDonationAddress will synchronously run a 'server.donation_address' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#server-donation-address
func (c *Client) ServerDonationAddress() (string, error) {
	res, err := c.syncRequest(c.req("server.donation_address"))
	if err != nil {
		return "", err
	}

	if res.Error != nil {
		return "", errors.New(res.Error.Message)
	}

	return res.Result.(string), nil
}

// ServerFeatures returns a list of features and services supported by the server
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#server-donation-address
func (c *Client) ServerFeatures() (*ServerInfo, error) {
	info := new(ServerInfo)
	switch c.Protocol {
	case Protocol10:
		return nil, ErrUnavailableMethod
	default:
		res, err := c.syncRequest(c.req("server.features"))
		if err != nil {
			return nil, err
		}

		if res.Error != nil {
			return nil, errors.New(res.Error.Message)
		}

		b, err := json.Marshal(res.Result)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &info); err != nil {
			return nil, err
		}
	}
	return info, nil
}

// ServerPeers returns a list of peer servers
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#server-peers-subscribe
func (c *Client) ServerPeers() (peers []*Peer, err error) {
	res, err := c.syncRequest(c.req("server.peers.subscribe"))
	if err != nil {
		return
	}

	if res.Error != nil {
		err = errors.New(res.Error.Message)
		return
	}

	var list []interface{}
	b, err := json.Marshal(res.Result)
	if err != nil {
		return
	}
	if err = json.Unmarshal(b, &list); err != nil {
		return
	}

	for _, l := range list {
		p := &Peer{
			Address: l.([]interface{})[0].(string),
			Name:    l.([]interface{})[1].(string),
		}
		b, err := json.Marshal(l.([]interface{})[2])
		if err != nil {
			continue
		}
		if err = json.Unmarshal(b, &p.Features); err != nil {
			continue
		}
		peers = append(peers, p)
	}
	return
}

// ScriptHashBalanceBalance will synchronously run a 'blockchain.scripthash.get_balance' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-scripthash-get-balance
func (c *Client) ScriptHashBalance(scriptHash string) (*Balance, error) {
	balance := new(Balance)

	res, err := c.syncRequest(c.req("blockchain.scripthash.get_balance", scriptHash))
	if err != nil {
		return nil, fmt.Errorf("error getting balance for scripthash %s: %w", scriptHash, err)
	}

	if res.Error != nil {
		return nil, fmt.Errorf("error getting balance for scripthash %s: %w", scriptHash, errors.New(res.Error.Message))
	}

	b, err := json.Marshal(res.Result)
	if err != nil {
		return nil, fmt.Errorf("error getting balance for scripthash %s: %w", scriptHash, err)
	}

	if err = json.Unmarshal(b, balance); err != nil {
		return nil, fmt.Errorf("error getting balance for scripthash %s: %w", scriptHash, err)
	}

	return balance, nil
}

// ScriptHashHistory will synchronously run a 'blockchain.scripthash.get_history' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-scripthash-get-history
func (c *Client) ScriptHashHistory(scriptHash string) ([]Tx, error) {
	list := []Tx{}

	res, err := c.syncRequest(c.req("blockchain.scripthash.get_history", scriptHash))
	if err != nil {
		return nil, fmt.Errorf("error getting history for scripthash %s: %w", scriptHash, err)
	}

	if res.Error != nil {
		return nil, fmt.Errorf("error getting history for scripthash %s: %w", scriptHash, errors.New(res.Error.Message))
	}

	b, err := json.Marshal(res.Result)
	if err != nil {
		return nil, fmt.Errorf("error getting history for scripthash %s: %w", scriptHash, err)
	}

	if err = json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("error getting history for scripthash %s: %w", scriptHash, err)
	}

	return list, nil
}

// ScriptHashMempool will synchronously run a 'blockchain.scripthash.get_mempool' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-scripthash-get-mempool
func (c *Client) ScriptHashMempool(scripthash string) ([]MempoolTx, error) {
	list := []MempoolTx{}

	res, err := c.syncRequest(c.req("blockchain.scripthash.get_mempool", scripthash))
	if err != nil {
		return nil, fmt.Errorf("error getting mempool for scripthash %s: %w", scripthash, err)
	}

	if res.Error != nil {
		err = errors.New(res.Error.Message)
		return nil, fmt.Errorf("error getting mempool for scripthash %s: %w", scripthash, err)
	}

	b, err := json.Marshal(res.Result)
	if err != nil {
		return nil, fmt.Errorf("error getting mempool for scripthash %s: %w", scripthash, err)
	}
	if err = json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("error getting mempool for scripthash %s: %w", scripthash, err)
	}
	return list, nil
}

// ScriptHashListUnspent will synchronously run a 'blockchain.scripthash.listunspent' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-scripthash-listunspent
func (c *Client) ScriptHashListUnspent(scripthash string) ([]UnspentTx, error) {
	list := []UnspentTx{}

	res, err := c.syncRequest(c.req("blockchain.scripthash.listunspent", scripthash))
	if err != nil {
		return nil, fmt.Errorf("error getting listunspent for scripthash %s: %w", scripthash, err)
	}

	if res.Error != nil {
		err = errors.New(res.Error.Message)
		return nil, fmt.Errorf("error getting listunspent for scripthash %s: %w", scripthash, err)
	}

	b, err := json.Marshal(res.Result)
	if err != nil {
		return nil, fmt.Errorf("error getting listunspent for scripthash %s: %w", scripthash, err)
	}
	if err = json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("error getting listunspent for scripthash %s: %w", scripthash, err)
	}
	return list, nil
}

// BlockHeader will synchronously run a 'blockchain.block.header' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-block-header

func (c *Client) BlockHeader(index int) (header *BlockHeader, err error) {
	res, err := c.syncRequest(c.req("blockchain.block.header", index, index+1))
	if err != nil {
		return
	}

	if res.Error != nil {
		err = errors.New(res.Error.Message)
		return
	}

	b, err := json.Marshal(res.Result)
	if err != nil {
		return
	}
	if err = json.Unmarshal(b, &header); err != nil {
		return
	}
	return
}

// BroadcastTransaction will synchronously run a 'blockchain.transaction.broadcast' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-transaction-broadcast
func (c *Client) BroadcastTransaction(hex string) (string, error) {
	res, err := c.syncRequest(c.req("blockchain.transaction.broadcast", hex))
	if err != nil {
		return "", err
	}

	if res.Result == nil || strings.Contains(res.Result.(string), "rejected") {
		return "", ErrRejectedTx
	}

	return res.Result.(string), nil
}

// GetTransaction will synchronously run a 'blockchain.transaction.get' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain.transaction.get
func (c *Client) GetTransaction(hash string) (string, error) {
	res, err := c.syncRequest(c.req("blockchain.transaction.get", hash))
	if err != nil {
		return "", err
	}

	if res.Error != nil {
		return "", errors.New(res.Error.Message)
	}

	return res.Result.(string), nil
}

func (c *Client) GetVerboseTransaction(hash string) (*VerboseTx, error) {
	tx := new(VerboseTx)

	if ok := c.txCache.Load(hash, tx); ok {
		c.debug("Tx %s found in cache", hash)
		return tx, nil
	}

	res, err := c.syncRequest(c.req("blockchain.transaction.get", hash, true))
	if err != nil {
		return nil, fmt.Errorf("error getting verbose transaction %s: %w", hash, err)
	}

	if res.Error != nil {
		return nil, fmt.Errorf("error getting verbose transaction %s: %w", hash, errors.New(res.Error.Message))
	}

	b, err := json.Marshal(res.Result)
	if err != nil {
		return nil, fmt.Errorf("error getting verbose transaction %s: %w", hash, err)
	}

	if err = json.Unmarshal(b, tx); err != nil {
		return nil, fmt.Errorf("error getting verbose transaction %s: %w", hash, err)
	}

	if tx.Confirmations > 0 {
		err := c.txCache.Store(hash, *tx)
		if err != nil {
			c.debug("Store tx %s in cache failed: %v", hash, err)
		}
	}

	return tx, nil
}

// EstimateFee will synchronously run a 'blockchain.estimatefee' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-estimatefee
func (c *Client) EstimateFee(blocks int) (float64, error) {
	res, err := c.syncRequest(c.req("blockchain.estimatefee", strconv.Itoa(blocks)))
	if err != nil {
		return 0, err
	}

	if res.Error != nil {
		return 0, errors.New(res.Error.Message)
	}

	return res.Result.(float64), nil
}

// TransactionMerkle will synchronously run a 'blockchain.transaction.get_merkle' operation
//
// https://electrumx.readthedocs.io/en/latest/protocol-methods.html#blockchain-transaction-get-merkle
func (c *Client) TransactionMerkle(tx string, height int) (tm *TxMerkle, err error) {
	res, err := c.syncRequest(c.req("blockchain.transaction.get_merkle", tx, strconv.Itoa(height)))
	if err != nil {
		return
	}

	if res.Error != nil {
		err = errors.New(res.Error.Message)
		return
	}

	b, err := json.Marshal(res.Result)
	log.Printf("%s", res.Result)
	if err != nil {
		return
	}
	if err = json.Unmarshal(b, &tm); err != nil {
		return
	}
	return
}

// GetVerboseTransactionBatch gets the VerboseTx from a batch of transactions.
func (c *Client) GetVerboseTransactionBatch(
	hashes []string,
) ([]*VerboseTx, error) {
	txs := make([]*VerboseTx, 0, len(hashes))

	params := make([][]any, len(hashes))
	for i, h := range hashes {
		params[i] = []any{h, true}
	}

	res, err := c.syncBatchRequest(c.batchReq("blockchain.transaction.get", params))
	if err != nil {
		return nil, err
	}

	for _, r := range res {
		if r.Error != nil {
			return nil, errors.New(r.Error.Message)
		}

		tx := new(VerboseTx)

		b, err := json.Marshal(r.Result)
		if err != nil {
			return nil, err
		}

		if err = json.Unmarshal(b, tx); err != nil {
			return nil, err
		}

		txs = append(txs, tx)
	}
	return txs, nil
}

func (c *Client) EnrichVin(vins []Vin) ([]VinWithPrevout, error) {
	hashes := make([]string, len(vins))

	for i, vin := range vins {
		hashes[i] = vin.TxID
	}

	vinWithPrevouts := make([]VinWithPrevout, len(vins))

	for i := 0; i <= len(hashes)/DefaultBatchSize; i++ {
		start := i * DefaultBatchSize

		end := start + DefaultBatchSize

		if end > len(hashes) {
			end = len(hashes)
		}

		batchHashes := hashes[start:end]
		if len(batchHashes) == 0 {
			break
		}

		txs, err := c.GetVerboseTransactionBatch(batchHashes)
		if err != nil {
			return nil, err
		}

		for j, tx := range txs {
			vinIndex := start + j
			vinWithPrevouts[vinIndex] = VinWithPrevout{
				Vin:     &vins[vinIndex],
				Prevout: &tx.Vout[vins[vinIndex].Vout],
			}
		}
	}

	return vinWithPrevouts, nil
}

// Details a transaction by adding Prevout to Vin.
func (c *Client) EnrichTransaction(tx *VerboseTx, blockHeight int64) (*RichTx, error) {
	richTx := RichTx{
		VerboseTx:    *tx,
		Vin:          []VinWithPrevout{}, // empty now
		InputsTotal:  0,
		OutputsTotal: 0,
		FeeInSat:     0,
		Height:       blockHeight,
		Fee:          0,
	}

	if ok := c.txCache.Load(tx.TxID, &richTx); ok {
		return &richTx, nil
	}

	vinWithPrevouts, err := c.EnrichVin(tx.Vin)
	if err != nil {
		return nil, err
	}

	richTx.Vin = vinWithPrevouts

	// calculate inputsTotal
	for _, vout := range tx.Vout {
		richTx.OutputsTotal += vout.Value
	}

	// calculate outputsTotal
	for _, vin := range richTx.Vin {
		richTx.InputsTotal += vin.Prevout.Value
	}

	// calculate fee
	richTx.FeeInSat = int64(richTx.InputsTotal - richTx.OutputsTotal)

	richTx.Fee = float64(richTx.FeeInSat) / BTCDecimals

	err = c.txCache.Store(tx.TxID, richTx)
	if err != nil {
		c.debug("Store detailedTx %s in cache failed: %v", tx.TxID, err)
	}

	return &richTx, nil
}
