package werc20

import (
	"embed"
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"
	erc20 "github.com/cosmos/evm/precompiles/erc20"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	transferkeeper "github.com/cosmos/evm/x/ibc/transfer/keeper"
)

// abiPath defines the path to the WERC-20 precompile ABI JSON file.
const abiPath = "abi.json"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

var _ vm.PrecompiledContract = &Precompile{}

// Precompile defines the precompiled contract for WERC20.
type Precompile struct {
	*erc20.Precompile
}

const (
	// DepositRequiredGas defines the gas required for the Deposit transaction.
	DepositRequiredGas uint64 = 23_878
	// WithdrawRequiredGas defines the gas required for the Withdraw transaction.
	WithdrawRequiredGas uint64 = 9207
)

// LoadABI loads the IWERC20 ABI from the embedded abi.json file
// for the werc20 precompile.
func LoadABI() (abi.ABI, error) {
	return cmn.LoadABI(f, abiPath)
}

// NewPrecompile creates a new WERC20 Precompile instance implementing the
// PrecompiledContract interface. This type wraps around the ERC20 Precompile
// instance to provide additional methods.
func NewPrecompile(
	tokenPair erc20types.TokenPair,
	bankKeeper cmn.BankKeeper,
	erc20Keeper Erc20Keeper,
	transferKeeper transferkeeper.Keeper,
) (*Precompile, error) {
	newABI, err := LoadABI()
	if err != nil {
		return nil, fmt.Errorf("error loading the ABI: %w", err)
	}

	erc20Precompile, err := erc20.NewPrecompile(tokenPair, bankKeeper, erc20Keeper, transferKeeper)
	if err != nil {
		return nil, fmt.Errorf("error instantiating the ERC20 precompile: %w", err)
	}

	// use the IWERC20 ABI
	erc20Precompile.ABI = newABI

	return &Precompile{
		Precompile: erc20Precompile,
	}, nil
}

// Address returns the address of the WERC20 precompiled contract.
func (p Precompile) Address() common.Address {
	return p.Precompile.Address()
}

// RequiredGas calculates the contract gas use.
func (p Precompile) RequiredGas(input []byte) uint64 {
	// TODO: these values were obtained from Remix using the WEVMOS9.sol.
	// We should execute the transactions from Cosmos EVM testnet
	// to ensure parity in the values.

	// If there is no method ID, then it's the fallback or receive case
	if len(input) < 4 {
		return DepositRequiredGas
	}

	methodID := input[:4]
	method, err := p.MethodById(methodID)
	if err != nil {
		return 0
	}

	switch method.Name {
	case DepositMethod:
		return DepositRequiredGas
	case WithdrawMethod:
		return WithdrawRequiredGas
	default:
		return p.Precompile.RequiredGas(input)
	}
}

// Run executes the precompiled contract WERC20 methods defined in the ABI.
func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) (bz []byte, err error) {
	bz, err = p.run(evm, contract, readOnly)
	if err != nil {
		return cmn.ReturnRevertError(evm, err)
	}

	return bz, nil
}

func (p Precompile) run(evm *vm.EVM, contract *vm.Contract, readOnly bool) (bz []byte, err error) {
	ctx, stateDB, method, initialGas, args, err := p.RunSetup(evm, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}

	// This handles any out of gas errors that may occur during the execution of
	// a precompile tx or query. It avoids panics and returns the out of gas error so
	// the EVM can continue gracefully.
	defer cmn.HandleGasError(ctx, contract, initialGas, &err)()

	switch {
	case method.Type == abi.Fallback,
		method.Type == abi.Receive,
		method.Name == DepositMethod:
		bz, err = p.Deposit(ctx, contract, stateDB)
	case method.Name == WithdrawMethod:
		bz, err = p.Withdraw(ctx, contract, stateDB, args)
	default:
		// ERC20 transactions and queries
		bz, err = p.HandleMethod(ctx, contract, stateDB, method, args)
	}

	if err != nil {
		return nil, err
	}

	cost := ctx.GasMeter().GasConsumed() - initialGas

	if !contract.UseGas(cost, nil, tracing.GasChangeCallPrecompiledContract) {
		return nil, vm.ErrOutOfGas
	}

	return bz, nil
}

// IsTransaction returns true if the given method name correspond to a
// transaction. Returns false otherwise.
func (p Precompile) IsTransaction(method *abi.Method) bool {
	txMethodName := []string{DepositMethod, WithdrawMethod}
	txMethodType := []abi.FunctionType{abi.Fallback, abi.Receive}

	if slices.Contains(txMethodName, method.Name) || slices.Contains(txMethodType, method.Type) {
		return true
	}

	return p.Precompile.IsTransaction(method)
}
