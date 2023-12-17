package electrum

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	const testAddress = "1ErbiumBjW4ScHNhLCcNWK5fFsKFpsYpWb"
	const testServer = "electrum.bitaroo.net:50002" // erbium1.sytes.net:50002 | ex-btc.server-on.net:50002

	t.Run("Protocol_1.4", func(t *testing.T) {
		client, err := New(&Options{
			Address:   testServer,
			TLS:       &tls.Config{InsecureSkipVerify: true},
			Protocol:  Protocol14_2,
			KeepAlive: true,
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

		// t.Run("AddressBalance", func(t *testing.T) {
		// 	balance, err := client.AddressBalance(testAddress)
		// 	if err != nil {
		// 		t.Error(err)
		// 		return
		// 	}
		// 	log.Printf("Balance: %+v\n", balance)
		// })

		// t.Run("AddressMempool", func(t *testing.T) {
		// 	mempool, err := client.AddressMempool(testAddress)
		// 	if err != nil {
		// 		t.Error(err)
		// 		return
		// 	}
		// 	log.Printf("Mempool: %+v\n", mempool)
		// })

		// t.Run("AddressHistory", func(t *testing.T) {
		// 	history, err := client.AddressHistory(testAddress)
		// 	if err != nil {
		// 		t.Error(err)
		// 		return
		// 	}
		// 	log.Printf("History: %+v\n", history)
		// })

		// t.Run("AddressListUnspent", func(t *testing.T) {
		// 	utxo, err := client.AddressListUnspent(testAddress)
		// 	if err != nil {
		// 		t.Error(err)
		// 		return
		// 	}
		// 	log.Printf("Unspent: %+v\n", utxo)
		// })

		// t.Run("BlockHeader", func(t *testing.T) {
		// 	header, err := client.BlockHeader(56770)
		// 	if err != nil {
		// 		t.Error(err)
		// 		return
		// 	}
		// 	log.Printf("Header: %+v\n", header)
		// })

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

		t.Run("TransactionMerkle", func(t *testing.T) {
			m, err := client.TransactionMerkle("c011c74e1d0938003fbcd25ce8f60343766a5665da8a57412c50a9029b5f0056", 522232)
			if err != nil {
				t.Error(err)
				return
			}
			log.Printf("%v", m)
		})

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
		Address: "electrum.bitaroo.net:50002",
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
		Address: "electrum.bitaroo.net:50002",
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
