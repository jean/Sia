package wallet

import (
	"testing"
	"time"

	"github.com/NebulousLabs/Sia/types"
)

// TestDefragWallet mines many blocks and checks that the wallet's outputs are
// consolidated once more than defragThreshold blocks are mined.
func TestDefragWallet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	wt, err := createWalletTester("TestDefragWallet")
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	// mine defragThreshold blocks, resulting in defragThreshold outputs
	for i := 0; i < defragThreshold; i++ {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// add another block to push the number of outputs over the threshold
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// allow some time for the defrag transaction to occur, then mine another block
	time.Sleep(time.Second)

	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// defrag should keep the outputs below the threshold
	if len(wt.wallet.siacoinOutputs) > defragThreshold {
		t.Fatalf("defrag should result in fewer than defragThreshold outputs, got %v wanted %v\n", len(wt.wallet.siacoinOutputs), defragThreshold)
	}
}

// TestDefragWalletDust verifies that dust outputs do not trigger the defrag
// operation.
func TestDefragWalletDust(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	wt, err := createWalletTester("TestDefragWalletDust")
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	dustOutputValue := types.NewCurrency64(10000)
	noutputs := defragThreshold + 1

	tbuilder := wt.wallet.StartTransaction()
	err = tbuilder.FundSiacoins(dustOutputValue.Mul64(uint64(noutputs)))
	if err != nil {
		t.Fatal(err)
	}

	var dest types.UnlockHash
	for k := range wt.wallet.keys {
		dest = k
		break
	}

	for i := 0; i < noutputs; i++ {
		tbuilder.AddSiacoinOutput(types.SiacoinOutput{
			Value:      dustOutputValue,
			UnlockHash: dest,
		})
	}

	txns, err := tbuilder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	err = wt.tpool.AcceptTransactionSet(txns)
	if err != nil {
		t.Fatal(err)
	}

	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Second)

	if len(wt.wallet.siacoinOutputs) < defragThreshold {
		t.Fatal("defrag consolidated dust outputs")
	}
}

// TestDefragOutputExhaustion verifies that sending transactions still succeeds
// even when the defragger is under heavy stress.
func TestDefragOutputExhaustion(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	t.Skip("skipping until consensus consistency bug is fixed")

	wt, err := createWalletTester("TestDefragOutputExhaustion")
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	wt.wallet.mu.Lock()
	var dest types.UnlockHash
	for k := range wt.wallet.keys {
		dest = k
		break
	}
	wt.wallet.mu.Unlock()

	wt.miner.AddBlock()

	// concurrently make a bunch of transactions with lots of outputs to keep the
	// defragger running
	closechan := make(chan struct{})
	donechan := make(chan struct{})
	go func() {
		defer close(donechan)
		for {
			select {
			case <-closechan:
				return
			case <-time.After(time.Millisecond * 100):
				wt.miner.AddBlock()
				txnValue := types.SiacoinPrecision.Mul64(3000)
				fee := types.SiacoinPrecision.Mul64(10)
				numOutputs := defragThreshold + 1

				tbuilder := wt.wallet.StartTransaction()
				tbuilder.FundSiacoins(txnValue.Mul64(uint64(numOutputs)).Add(fee))

				for i := 0; i < numOutputs; i++ {
					tbuilder.AddSiacoinOutput(types.SiacoinOutput{
						Value:      txnValue,
						UnlockHash: dest,
					})
				}

				tbuilder.AddMinerFee(fee)

				txns, err := tbuilder.Sign(true)
				if err != nil {
					t.Error("Error signing fragmenting transaction:", err)
				}
				err = wt.tpool.AcceptTransactionSet(txns)
				if err != nil {
					t.Error("Error accepting fragmenting transaction:", err)
				}
				wt.miner.AddBlock()
			}
		}
	}()

	time.Sleep(time.Second * 1)

	// ensure we can still send transactions while receiving aggressively
	// fragmented outputs
	for i := 0; i < 30; i++ {
		sendAmount := types.SiacoinPrecision.Mul64(2000)
		_, err = wt.wallet.SendSiacoins(sendAmount, types.UnlockHash{})
		if err != nil {
			t.Errorf("%v: %v", i, err)
		}
		time.Sleep(time.Millisecond * 50)
	}

	close(closechan)
	<-donechan
}