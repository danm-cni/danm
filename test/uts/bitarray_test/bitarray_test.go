package bitarray_test

import (
	"errors"
	"math"
	"net"
	"strconv"
	"testing"

	"github.com/danm-cni/danm/pkg/bitarray"
)

var arraySizeTestConsts = []struct {
	inputSize       uint32
	isErrorExpected bool
	expectedSize    uint32
}{
	{0, false, 0},
	{1, false, 1},
	{33000, false, 33000},
}

var createBaFromNetTcs = []struct {
	name            string
	subnet          string
	isErrorExpected bool
	expectedSize    uint32
}{
	{"maxIpV4", "10.0.0.0/9", false, uint32(math.Pow(2, float64(bitarray.MaxSupportedAllocLength)))},
	{"minIpV4", "192.168.1.50/32", false, 1},
	{"negativeIpV4", "192.168.1.50/33", false, 0},
	{"overTheLimitIpV4", "10.0.0.0/8", true, 0},
	{"maxIpV6", "2001:db8:85a3::8a2e:370:7334/105", false, uint32(math.Pow(2, float64(bitarray.MaxSupportedAllocLength)))},
	{"minIpV6", "2001:db8:85a3::8a2e:370:7334/128", false, 1},
	{"overTheLimitIpV6", "2001:db8:85a3::8a2e:370:7334/104", true, 0},
}

func TestNewBitArray(t *testing.T) {
	for _, tt := range arraySizeTestConsts {
		t.Run("TestNewBitArray Size:"+strconv.Itoa(int(tt.inputSize)), func(t *testing.T) {
			testArray, newErr := bitarray.NewBitArray(tt.inputSize)
			err := evalBa(tt.isErrorExpected, newErr, tt.expectedSize, testArray)
			if err != nil {
				t.Errorf(err.Error())
			}
		})
	}
}

func TestBitArrayFunctionality(t *testing.T) {
	positionsToSet := [...]int{0, 4, 7}
	origBa, _ := bitarray.NewBitArray(8)
	for _, position := range positionsToSet {
		origBa.Set(uint32(position))
	}
	encodedBa := origBa.Encode()
	newBa := bitarray.NewBitArrayFromBase64(encodedBa)
	var isPositionSet bool
	for i := 0; i < 8; i++ {
		isPositionSet = newBa.Get(uint32(i))
		var shouldPositionBeSet bool
		for _, position := range positionsToSet {
			if i == position {
				shouldPositionBeSet = true
			}
		}
		if isPositionSet != shouldPositionBeSet {
			t.Errorf("After one round of Encoding + Decoding Bitarray field:%d changed value", i)
		}
	}
	for _, position := range positionsToSet {
		origBa.Reset(uint32(position))
		if origBa.Get(uint32(position)) {
			t.Errorf("Resetting did not work for number:%d", position)
		}
	}
}

func TestCreateBitArrayFromIpnet(t *testing.T) {
	for _, tc := range createBaFromNetTcs {
		t.Run(tc.name, func(t *testing.T) {
			_, subnet, _ := net.ParseCIDR(tc.subnet)
			ba, newErr := bitarray.CreateBitArrayFromIpnet(subnet)
			err := evalBa(tc.isErrorExpected, newErr, tc.expectedSize, ba)
			if err != nil {
				t.Errorf(err.Error())
			}
		})
	}
}

func evalBa(isErrorExpected bool, err error, expSize uint32, testArray *bitarray.BitArray) error {
	if (isErrorExpected && nil == err) && (!isErrorExpected && nil != err) {
		return errors.New("BitArray initialization returned unexpected error result at test value " + strconv.Itoa(int(expSize)) + ": error expected: " + strconv.FormatBool(isErrorExpected) + ", returned error" + err.Error())
	}
	if isErrorExpected {
		return nil
	}
	actualSize := testArray.Len()
	if actualSize != expSize {
		return errors.New("BitArray returned unexpected size: " + strconv.Itoa(int(actualSize)) + ", expected size was:" + strconv.Itoa(int(expSize)))
	}
	return nil
}
