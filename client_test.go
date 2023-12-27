package electrum

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

const testServer = "electrum.bitaroo.net:50002" // erbium1.sytes.net:50002 | ex-btc.server-on.net:50002

func TestClient(t *testing.T) {
	const testAddress = "1ErbiumBjW4ScHNhLCcNWK5fFsKFpsYpWb"
	const testScriptHash = "69960ffb520c7662430d15d9c0a75adc204619f562c6a9316288ec8bb4e288a5"

	t.Run("Protocol_1.4", func(t *testing.T) {
		client, err := New(&Options{
			Address:   testServer,
			TLS:       &tls.Config{InsecureSkipVerify: true},
			Protocol:  Protocol14_2,
			KeepAlive: true,
			Log:       log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile),
		})
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()

		t.Run("ServerPing", func(t *testing.T) {
			err := client.ServerPing()
			if err != nil && !strings.Contains(err.Error(), "unknown method") {
				t.Error(err)
				return
			}
			log.Println("Pong")
		})

		t.Run("ServerVersion", func(t *testing.T) {
			res, err := client.ServerVersion()
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Server Software: %s\n", res.Software)
			log.Printf("Server Protocol: %s\n", res.Protocol)
		})

		t.Run("ServerBanner", func(t *testing.T) {
			res, err := client.ServerBanner()
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Server Banner: %s\n", res)
		})

		t.Run("ServerDonationAddress", func(t *testing.T) {
			res, err := client.ServerDonationAddress()
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Server Donation Address: %s\n", res)
		})

		t.Run("ServerFeatures", func(t *testing.T) {
			res, err := client.ServerFeatures()
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Genesis Hash: %s", res.GenesisHash)
			log.Printf("Hash Function: %s", res.HashFunction)
			log.Printf("Max Protocol: %s", res.ProtocolMax)
			log.Printf("Min Protocol: %s", res.ProtocolMin)
		})

		t.Run("ServerPeers", func(t *testing.T) {
			peers, err := client.ServerPeers()
			if err != nil {
				t.Error(err)
				return
			}
			log.Println("Server peers")
			for _, p := range peers {
				log.Printf("%s > %s (%v)", p.Name, p.Address, p.Features)
			}
		})

		t.Run("ScriptHashBalance", func(t *testing.T) {
			balance, err := client.ScriptHashBalance(testScriptHash)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Balance: %+v\n", balance)
		})

		t.Run("ScriptHashMempool", func(t *testing.T) {
			mempool, err := client.ScriptHashMempool(testScriptHash)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Mempool: %+v\n", mempool)
		})

		t.Run("ScriptHashHistory", func(t *testing.T) {
			history, err := client.ScriptHashHistory(testScriptHash)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("History: %+v\n", history)
		})

		t.Run("ScriptHashListUnspent", func(t *testing.T) {
			utxo, err := client.ScriptHashListUnspent(testScriptHash)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Unspent: %+v\n", utxo)
		})

		t.Run("BlockHeader", func(t *testing.T) {
			header, err := client.BlockHeader(56770)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Header: %+v\n", header)
		})

		t.Run("BroadcastTransaction", func(t *testing.T) {
			res, err := client.BroadcastTransaction("01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff0702621b03cfd201ffffffff010000000000000000016a00000000")
			if err == nil {
				t.Error(errors.New("unexpected result"))
				return
			}
			log.Printf("%+v\n", res)
		})

		t.Run("GetTransaction", func(t *testing.T) {
			res, err := client.GetTransaction("4f73e43b92d337da8e69417601de1476bd7577cbac901fa28dba37ce1362adb9")
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Tx: %+v\n", res)
		})

		t.Run("EstimateFee", func(t *testing.T) {
			fee, err := client.EstimateFee(6)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("Fee: %+v\n", fee)
		})

		t.Run("NotifyBlockHeaders", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			headers, err := client.NotifyBlockHeaders(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			for {
				select {
				case h := <-headers:
					log.Printf("%+v\n", h)
				case <-ctx.Done():
					return
				}
			}
		})

		t.Run("NotifyAddressTransactions", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			txs, err := client.NotifyAddressTransactions(ctx, testAddress)
			if err != nil {
				t.Error(err)
				return
			}
			for {
				select {
				case t := <-txs:
					log.Printf("%+v\n", t)
				case <-ctx.Done():
					return
				}
			}
		})

		// t.Run("TransactionMerkle", func(t *testing.T) {
		// 	m, err := client.TransactionMerkle("c011c74e1d0938003fbcd25ce8f60343766a5665da8a57412c50a9029b5f0056", 522232)
		// 	if err != nil {
		// 		t.Error(err)
		// 		return
		// 	}
		// 	log.Printf("%v", m)
		// })
		//
		// Deprecated methods

		t.Run("UTXOAddress", func(t *testing.T) {
			_, err := client.UTXOAddress("4f73e43b92d337da8e69417601de1476bd7577cbac901fa28dba37ce1362adb9")
			if err != ErrDeprecatedMethod {
				t.Error(err)
			}
		})

		t.Run("BlockChunk", func(t *testing.T) {
			_, err := client.BlockChunk(7777)
			if err != ErrDeprecatedMethod {
				t.Error(errors.New("unexpected result"))
			}
		})

		t.Run("NotifyBlockNums", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			nums, err := client.NotifyBlockNums(ctx)
			if err != ErrDeprecatedMethod {
				t.Error(err)
				return
			}
			for {
				select {
				case n := <-nums:
					log.Printf("%+v\n", n)
				case <-ctx.Done():
					return
				}
			}
		})
	})
}

func ExampleClient_ServerVersion() {
	client, err := New(&Options{
		Address: testServer,
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()
	version, err := client.ServerVersion()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(version.Software)
	// Output: ElectrumX 1.16.0
}

func ExampleClient_ServerDonationAddress() {
	client, err := New(&Options{
		Address: testServer,
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()
	addr, err := client.ServerDonationAddress()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(addr)
	// Output: 36UgQmHjUainV6B7HV58vmV3gAc1w3Rurt
}

func ExampleClient_ScriptHashBalance() {
	client, err := New(&Options{
		Address: testServer,
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()
	// address: 13aoDNsMJ8w1EAvT8LkH8WnAYrygBAUUF1
	balance, err := client.ScriptHashBalance("9e973caf3b602db193b420497a043ea99225976eff5a33037a107ac291882c70")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(balance.Confirmed)
	// Output: 582991
}

func ExampleClient_ScriptHashHistory() {
	client, err := New(&Options{
		Address: testServer,
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()
	// address: 13aoDNsMJ8w1EAvT8LkH8WnAYrygBAUUF1
	history, err := client.ScriptHashHistory("9e973caf3b602db193b420497a043ea99225976eff5a33037a107ac291882c70")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println((history)[0].Height)
	// Output: 768424
}

func ExampleClient_GetVerboseTransaction() {
	client, err := New(&Options{
		Address: testServer,
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()
	tx, err := client.GetVerboseTransaction("4f73e43b92d337da8e69417601de1476bd7577cbac901fa28dba37ce1362adb9")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%d %d\n", tx.Blocktime, len(tx.Vin))
	// Output: 1512206656 1
}

func ExampleClient_EnrichTransaction() {
	client, err := New(&Options{
		Address: "reports-electrumx1.triple-a.xyz:50002",
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
		// Log: log.New(os.Stderr, "ExampleClient_EnrichVin: ", log.LstdFlags|log.Lshortfile),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()
	tx, err := client.GetVerboseTransaction("5bd5c43f112181786312711e505aa68a95f513cf0db9b736f52e5860666752f2")
	if err != nil {
		fmt.Println(err)
		return
	}
	richTx, err := client.EnrichTransaction(tx)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(richTx.Vin[0].Prevout.Value, richTx.Vin[0].Prevout.ScriptPubKey.Address)
	fmt.Println(richTx.Vin[590].Prevout.Value, richTx.Vin[590].Prevout.ScriptPubKey.Address)
	// Output: 0.0058323 3K1Jnpy5YVZjH9DCj6zmrDJ5mdsR68RjSu
	// 0.00584077 39NoF8tEtUwmnf2MAhvtU3ouEKEEQXYJHs
}

func ExampleClient_EnrichVin() {
	client, err := New(&Options{
		Address: "reports-electrumx1.triple-a.xyz:50002",
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
		Log: log.New(os.Stderr, "ExampleClient_EnrichVin: ", log.LstdFlags|log.Lshortfile),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	tx, err := client.GetVerboseTransaction("fcf4098faf20c19925f996eaef78b2c66dd6e37e1449f024823f6fd83454e25a")
	if err != nil {
		fmt.Println(err)
		return
	}
	vins, err := client.EnrichVin(tx.Vin)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println(len(vins))
	fmt.Println(vins[0].Prevout.Value, vins[0].TxID)
	fmt.Println(vins[11].Prevout.Value, vins[11].TxID)
	// Output: 12
	// 0.02930787 7aeb3f74c796b0637b4c06a8034315f698f9bc45e63eaebb4de6e8425dee4223
	// 0.02 b832e427e4f2104f400929e0b44db4c315e1d158dfe3e90b8eac616278681366
}
