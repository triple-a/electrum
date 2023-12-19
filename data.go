package electrum

import "encoding/json"

// VersionInfo contains the version information returned by the server
type VersionInfo struct {
	Software string `json:"software"`
	Protocol string `json:"protocol"`
}

// Host provides available endpoints for a given server
type Host struct {
	SSLPort uint `json:"ssl_port"`
	TCPPort uint `json:"tcp_port"`
}

// ServerInfo provides general information about the state and capabilities of the server
type ServerInfo struct {
	// A dictionary of endpoints that this server can be reached at. Normally this will only have a
	// single entry; other entries can be used in case there are other connection routes
	Hosts map[string]*Host `json:"hosts"`

	// The hash of the genesis block, can be used to detect if a peer is connected to one serving a different network
	GenesisHash string `json:"genesis_hash"`

	// The hash function the server uses for script hashing. The client must use this function to hash
	// pay-to-scripts to produce script hashes to send to the server
	HashFunction string `json:"hash_function"`

	// A string that identifies the server software
	ServerVersion string `json:"server_version"`

	// Max supported version of the protocol
	ProtocolMax string `json:"protocol_max"`

	// Min supported version of the protocol
	ProtocolMin string `json:"protocol_min"`
}

// Peer provides details of a known server node
type Peer struct {
	Address  string   `json:"address"`
	Name     string   `json:"name"`
	Features []string `json:"features"`
}

// Tx represents a transaction entry on the blockchain
type Tx struct {
	Hash   string `json:"tx_hash"`
	Pos    uint64 `json:"tx_pos"`
	Height uint64 `json:"height"`
	Value  uint64 `json:"value"`
}

type VerboseTx struct {
	Blockhash     string   `json:"blockhash"`
	Blocktime     uint64   `json:"blocktime"`
	Confirmations int32    `json:"confirmations"`
	Hash          string   `json:"hash"`
	Hex           string   `json:"hex"`
	Locktime      uint32   `json:"locktime"`
	Size          uint32   `json:"size"`
	Time          uint64   `json:"time"`
	TxID          string   `json:"txid"`
	Version       uint32   `json:"version"`
	Vin           []Vin    `json:"vin"`
	Vout          []Vout   `json:"vout"`
	Merkle        TxMerkle `json:"merkle,omitempty"` // For protocol v1.5 and up.
}

// ScriptPubKey represents the script of that transaction output.
type ScriptPubKey struct {
	Addresses []string `json:"addresses,omitempty"`
	Address   string   `json:"address,omitempty"`
	Asm       string   `json:"asm"`
	Hex       string   `json:"hex,omitempty"`
	ReqSigs   uint32   `json:"reqSigs,omitempty"`
	Type      string   `json:"type"`
}

// Vout represents the output side of a transaction.
type Vout struct {
	N            uint32       `json:"n"`
	ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
	Value        float64      `json:"value"`
}

// ScriptSig represents the signature script for that transaction input.
type ScriptSig struct {
	Asm string `json:"asm"`
	Hex string `json:"hex"`
}

// Vin represents the input side of a transaction.
type Vin struct {
	Coinbase  string    `json:"coinbase"`
	ScriptSig ScriptSig `json:"scriptSig"`
	Sequence  uint32    `json:"sequence"`
	TxID      string    `json:"txid"`
	Vout      uint32    `json:"vout"`
}

type VinWithPrevout struct {
	*Vin
	Prevout *Vout `json:"prevout"`
}

// RichTx represents a transaction entry on the blockchain with VinWithPrevout
type RichTx struct {
	VerboseTx
	Vin          []VinWithPrevout `json:"vin"`
	InputsTotal  float64          `json:"inputs_total"`
	OutputsTotal float64          `json:"outputs_total"`
	FeeInSat     float64          `json:"fee_in_sat"`
	Height       int64            `json:"height"`
	Fee          uint32           `json:"fee,omitempty"`
}

// TxMerkle provides the merkle branch of a given transaction
type TxMerkle struct {
	BlockHeight float64  `json:"block_height"`
	Pos         uint64   `json:"pos"`
	Merkle      []string `json:"merkle"`
}

// Balance show the funds available to an address, both
// confirmed and unconfirmed
type Balance struct {
	Confirmed   uint64 `json:"confirmed"`
	Unconfirmed uint64 `json:"unconfirmed"`
}

// BlockHeader display summarized details about an existing block in the chain
type BlockHeader struct {
	Branch []string `json:"branch"`
	Header string   `json:"header"`
	Root   string   `json:"root"`
}

type BlockHanders struct {
	Count   uint32   `json:"count"`
	Headers string   `json:"hex"`
	Max     uint32   `json:"max"`
	Branch  []string `json:"branch,omitempty"`
	Root    string   `json:"root,omitempty"`
}

// RPC error
type rpcError struct {
	Code    int64                  `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

// Protocol response structure
// http://docs.electrum.org/en/latest/protocol.html#response
type response struct {
	RPC    string      `json:"jsonrpc"`
	ID     int         `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
	Result interface{} `json:"result"`
	Error  *rpcError   `json:"error"`
}

// Protocol request structure
// http://docs.electrum.org/en/latest/protocol.html#request
type request struct {
	RPC    string `json:"jsonrpc"`
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params []any  `json:"params"`
}

// Properly encode a request object and append the message delimiter
func (r *request) encode() ([]byte, error) {
	if r.RPC == "" {
		r.RPC = "2.0"
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	b = append(b, delimiter)
	return b, nil
}
