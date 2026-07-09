package bitarray

import (
	b64 "encoding/base64"
	"errors"
	"math"
	"net"
	"strconv"

	"github.com/danm-cni/danm/pkg/datastructs"
)

const (
	MaxSupportedAllocLength = 23
)

// BitArray is type to represent an arbitrary long array of bits
type BitArray struct {
	len  uint32
	data []byte
}

// NewBitArray creates a new, empty BitArray object
// Returns error if length is zero, otherwise a pointer to the array
func NewBitArray(len uint32) (*BitArray, error) {
	if 0 == len {
		return nil, errors.New("Can't make a BitArray with zero length!")
	}
	bitArray := &BitArray{len, make([]byte, (len+7)/8)}
	bitArray.Set(0)
	return bitArray, nil
}

// NewBitArrayFromBase64 creates a new BitArray from a Base64 encoded string
func NewBitArrayFromBase64(text string) *BitArray {
	var tmp []byte
	tmp, _ = b64.StdEncoding.DecodeString(text)
	arr := new(BitArray)
	arr.len = uint32(len(tmp)) * 8
	arr.data = tmp
	return arr
}

func CreateBitArrayFromIpnet(ipnet *net.IPNet) (*BitArray, error) {
	if ipnet == nil {
		return nil, nil
	}
	maskSize, _ := ipnet.Mask.Size()
	baLength := datastructs.MinV4MaskLength - maskSize
	if ipnet.IP.To4() == nil {
		baLength = datastructs.MinV6PrefixLength - maskSize
	}
	if baLength > MaxSupportedAllocLength {
		return nil, errors.New("DANM does not support networks with more than 2^" + strconv.Itoa(MaxSupportedAllocLength) + " IP addresses")
	}
	bitArray, _ := NewBitArray(uint32(math.Pow(2, float64(baLength))))
	bitArray.Set(uint32(math.Pow(2, float64(baLength)) - 1))
	return bitArray, nil
}

// Set sets the bit at the input position of the BitArray
func (arr *BitArray) Set(pos uint32) {
	arr.data[pos/8] |= byte(0x1 << (7 - pos%8))
}

// Reset unsets the bit at the input position of the BitArray
func (arr *BitArray) Reset(pos uint32) {
	arr.data[pos/8] &= ^byte(0x1 << (7 - pos%8))
}

// Get returns whether the input position of the BitArray is set, or not
func (arr *BitArray) Get(pos uint32) bool {
	return (arr.data[pos/8] & (0x1 << (7 - pos%8))) != 0
}

// Encode returns the Base64 encoded string of the BitArray
func (arr *BitArray) Encode() string {
	return b64.StdEncoding.EncodeToString(arr.data)
}

// Len returns the length of the BitArray
func (arr *BitArray) Len() uint32 {
	if arr == nil {
		return 0
	}
	return arr.len
}
