// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package coding implements low-level QR coding details.
package coding // import "rsc.io/qr/coding"

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/text/encoding/japanese"
	"github.com/inkstray/rsc-qr/gf256"
)

// Field is the field for QR error correction.
var Field = gf256.NewField(0x11d, 2)

// A Version represents a QR version.
// The version specifies the size of the QR code:
// a QR code with version v has 4v+17 pixels on a side.
// Versions number from 1 to 40: the larger the version,
// the more information the code can store.
type Version int

const MinVersion = 1
const MaxVersion = 40

func (v Version) String() string {
	return strconv.Itoa(int(v))
}

func (v Version) sizeClass() int {
	if v <= 9 {
		return 0
	}
	if v <= 26 {
		return 1
	}
	return 2
}

// DataBytes returns the number of data bytes that can be
// stored in a QR code with the given version and level.
func (v Version) DataBytes(l Level) int {
	vt := &vtab[v]
	lev := &vt.level[l]
	return vt.bytes - lev.nblock*lev.check
}

// Encoding implements a QR data encoding scheme.
// The implementations--Numeric, Alphanumeric, and String--specify
// the character set and the mapping from UTF-8 to code bits.
// The more restrictive the mode, the fewer code bits are needed.
type Encoding interface {
	Check() error
	Bits(v Version) int
	Encode(b *Bits, v Version)
}

type Bits struct {
	b    []byte
	nbit int
}

func (b *Bits) Reset() {
	b.b = b.b[:0]
	b.nbit = 0
}

func (b *Bits) Bits() int {
	return b.nbit
}

func (b *Bits) Bytes() []byte {
	if b.nbit%8 != 0 {
		panic("fractional byte")
	}
	return b.b
}

func (b *Bits) Append(p []byte) {
	if b.nbit%8 != 0 {
		panic("fractional byte")
	}
	b.b = append(b.b, p...)
	b.nbit += 8 * len(p)
}

func (b *Bits) Write(v uint, nbit int) {
	for nbit > 0 {
		n := nbit
		if n > 8 {
			n = 8
		}
		if b.nbit%8 == 0 {
			b.b = append(b.b, 0)
		} else {
			m := -b.nbit & 7
			if n > m {
				n = m
			}
		}
		b.nbit += n
		sh := uint(nbit - n)
		b.b[len(b.b)-1] |= uint8(v >> sh << uint(-b.nbit&7))
		v -= v >> sh << sh
		nbit -= n
	}
}

// Num is the encoding for numeric data.
// The only valid characters are the decimal digits 0 through 9.
type Num string

func (s Num) String() string {
	return fmt.Sprintf("Num(%#q)", string(s))
}

func (s Num) Check() error {
	for _, c := range s {
		if c < '0' || '9' < c {
			return fmt.Errorf("non-numeric string %#q", string(s))
		}
	}
	return nil
}

var numLen = [3]int{10, 12, 14}

func (s Num) Bits(v Version) int {
	return 4 + numLen[v.sizeClass()] + (10*len(s)+2)/3
}

func (s Num) Encode(b *Bits, v Version) {
	b.Write(1, 4)
	b.Write(uint(len(s)), numLen[v.sizeClass()])
	var i int
	for i = 0; i+3 <= len(s); i += 3 {
		w := uint(s[i]-'0')*100 + uint(s[i+1]-'0')*10 + uint(s[i+2]-'0')
		b.Write(w, 10)
	}
	switch len(s) - i {
	case 1:
		w := uint(s[i] - '0')
		b.Write(w, 4)
	case 2:
		w := uint(s[i]-'0')*10 + uint(s[i+1]-'0')
		b.Write(w, 7)
	}
}

// Alpha is the encoding for alphanumeric data.
// The valid characters are 0-9A-Z$%*+-./: and space.
type Alpha string

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

func (s Alpha) String() string {
	return fmt.Sprintf("Alpha(%#q)", string(s))
}

func (s Alpha) Check() error {
	for _, c := range s {
		if strings.IndexRune(alphabet, c) < 0 {
			return fmt.Errorf("non-alphanumeric string %#q", string(s))
		}
	}
	return nil
}

var alphaLen = [3]int{9, 11, 13}

func (s Alpha) Bits(v Version) int {
	return 4 + alphaLen[v.sizeClass()] + (11*len(s)+1)/2
}

func (s Alpha) Encode(b *Bits, v Version) {
	b.Write(2, 4)
	b.Write(uint(len(s)), alphaLen[v.sizeClass()])
	var i int
	for i = 0; i+2 <= len(s); i += 2 {
		w := uint(strings.IndexRune(alphabet, rune(s[i])))*45 +
			uint(strings.IndexRune(alphabet, rune(s[i+1])))
		b.Write(w, 11)
	}

	if i < len(s) {
		w := uint(strings.IndexRune(alphabet, rune(s[i])))
		b.Write(w, 6)
	}
}

// String is the encoding for 8-bit data.  All bytes are valid.
type String string

func (s String) String() string {
	return fmt.Sprintf("String(%#q)", string(s))
}

func (s String) Check() error {
	return nil
}

var stringLen = [3]int{8, 16, 16}

func (s String) Bits(v Version) int {
	return 4 + stringLen[v.sizeClass()] + 8*len(s)
}

func (s String) Encode(b *Bits, v Version) {
	b.Write(4, 4)
	b.Write(uint(len(s)), stringLen[v.sizeClass()])
	for i := 0; i < len(s); i++ {
		b.Write(uint(s[i]), 8)
	}
}

// Kanji is the encoding for kanji.
// Valid characters are those in JIS X 0208.
type Kanji string

func (s Kanji) String() string {
	return fmt.Sprintf("Kanji(%#q)", string(s))
}

func (s Kanji) Check() error {
	// XXX not the best way to achieve this
	_, err := japanese.ShiftJIS.NewEncoder().String(string(s))
	if err != nil {
		err = fmt.Errorf("non-kanji string %#q", string(s))
	}
	return err
}

var kanjiLen = [3]int{8, 10, 12}

func (s Kanji) Bits(v Version) int {
	n := 4 + kanjiLen[v.sizeClass()]
	for range s {
		n += 13
	}
	return n
}

func (s Kanji) Encode(b *Bits, v Version) {
	k, err := japanese.ShiftJIS.NewEncoder().String(string(s))
	if err != nil || len(k)&1 != 0 {
		println("fail!", string(k), err.Error())
		return
	}
	b.Write(8, 4)
	b.Write(uint(len(k)/2), kanjiLen[v.sizeClass()])
	for i := 0; i < len(k); i += 2 {
		w := uint(k[i]&^0xc0)*0xc0 + uint(k[i+1]) - 0x100
		b.Write(w, 13)
	}
}

// A Pixel describes a single pixel in a QR code.
type Pixel uint32

func (p Pixel) Offset() uint {
	return uint(p >> 4)
}

func OffsetPixel(o uint) Pixel {
	return Pixel(o << 4)
}

func (r PixelRole) Pixel() Pixel {
	return Pixel(r)
}

func (p Pixel) Role() PixelRole {
	return PixelRole(p) & 15
}

func (p Pixel) String() string {
	s := p.Role().String()
	s += "+" + strconv.FormatUint(uint64(p.Offset()), 10)
	return s
}

// A PixelRole describes the role of a QR pixel.
type PixelRole uint32

const (
	_         PixelRole = iota
	Position            // position squares (large)
	Alignment           // alignment squares (small)
	Timing              // timing strip between position squares
	Format              // format metadata
	PVersion            // version pattern
	Unused              // unused pixel
	Data                // data bit
	Check               // error correction check bit
	Extra
)

var roles = []string{
	"",
	"position",
	"alignment",
	"timing",
	"format",
	"pversion",
	"unused",
	"data",
	"check",
	"extra",
}

func (r PixelRole) String() string {
	if Position <= r && r <= Check {
		return roles[r]
	}
	return strconv.Itoa(int(r))
}

// A Level represents a QR error correction level.
// From least to most tolerant of errors, they are L, M, Q, H.
type Level int

const (
	L Level = iota
	M
	Q
	H
)

func (l Level) String() string {
	if L <= l && l <= H {
		return "LMQH"[l : l+1]
	}
	return strconv.Itoa(int(l))
}

// A Code is a square pixel grid.
type Code struct {
	Bitmap []byte // 1 is black, 0 is white
	Size   int    // number of pixels on a side
	Stride int    // number of bytes per row
}

func (c *Code) Black(x, y int) bool {
	return 0 <= x && x < c.Size && 0 <= y && y < c.Size &&
		c.Bitmap[y*c.Stride+x/8]&(1<<uint(7-x&7)) != 0
}

func (c *Code) set(b []byte, y, x int) {
	b[y*c.Stride+x/8] |= 1 << (7 - x&7)
}

// Penalty calculates the penalty value for c.
func (c *Code) Penalty() int {
	// Total penalty is the sum of penalties for runs and boxes
	// of same-colour pixels, finder patterns and colour balance.
	//
	//   - RunP: for non-overlapping runs of n pixels, n>=5 -> n-2
	//   - BoxP: for possibly overlapping 2x2 boxes -> 3
	//   - FindP: for possibly overlapping finder patterns -> 40
	//     The pattern is 010111010 with 000 on either side,
	//     may extend into the quiet zone
	//   - BalP: for n% of black pixels -> 10*(celing(abs(n-50)/5)-1)
	//
	// https://www.nayuki.io/page/creating-a-qr-code-step-by-step
	const (
		MinRun    = 5             // RunP:  miniumu run length
		RunPDelta = -2            // RunP:  add to run length
		BoxPP     = 3             // BoxP:  points per box
		FindPP    = 40            // FindP: points per pattern
		BalPP     = 10            // BalP:  10 points
		BalPMul   = 20            //        for every 5% (100% / 20),
		BalPMax   = BalPMul/2 - 1 //        up to 9 times

		// last pixels are stored in a uint16 shifted left 4 bits,
		// to match against 12 bit finder patterns without masking.
		pShift = 16 - 12
		// finder patterns:
		FindB = uint16(0b0000_1011101_0 << pShift) // quiet zone before
		FindA = uint16(0b0_1011101_0000 << pShift) // quiet zone after
	)
	p := 0   // total penalty
	bal := 0 // black pixels

	// horizontal runs: RunP, FindP, BoxP and count black pixels for BalP
	for y := 0; y < c.Size; y++ {
		black := c.Black(0, y) // last pixel is black?
		r := 1                 // current run length for RunP
		var pat uint16         // last 12 pixels for FindP
		if black {
			pat = 1 << pShift
			bal++
		}
		// Scan rows from x=1.  BoxP is detected at the bottom right
		// pixel, RunP and FindP require even larger x.
		for x := 1; x < c.Size; x++ {
			if c.Black(x, y) != black {
				if r >= MinRun {
					p += r + RunPDelta // RunP
				}
				black = !black
				r = 0
			} else if y != 0 && c.Black(x-1, y-1) == black &&
				c.Black(x, y-1) == black {
				p += BoxPP // BoxP
			}
			pat <<= 1
			if black {
				pat |= 1 << pShift
				bal++
			} else if pat == FindB || pat == FindA {
				p += FindPP // FindP
			}
			r++
		}
		// handle last run
		if r >= MinRun {
			p += r + RunPDelta // RunP
		}
		// handle FindB with 1 pixel in the right quiet zone;
		// also includes FindA with 4 pixels in the quiet zone
		if pat <<= 1; pat == FindB {
			p += 2 * FindPP // 2×FindP
		} else {
			// handle FindA with 1-4 pixels in quiet zone
			switch FindA {
			case pat, pat << 1, pat << 2, pat << 3:
				p += FindPP // FindP
			}
		}
	}

	// calculate BalP
	// Exact percentages get less penalty.  E.g., 40% and 60% get
	// 10 points like 41%, not 20 like 39%.  To round away from 50%,
	// fold bal into 0 <= n < c.Size²/2 and divide rounding down.
	// No need to handle 50% as c.Size is always odd.
	sq := c.Size * c.Size
	if bal > sq/2 {
		bal = sq - bal
	}
	p += (BalPMax - (bal * BalPMul / sq)) * BalPP

	// vertical runs: RunP, FindP
	for x := 0; x < c.Size; x++ {
		black := c.Black(x, 0)
		r := 1
		var pat uint16
		if black {
			pat = 1 << pShift
		}
		for y := 1; y < c.Size; y++ {
			if c.Black(x, y) != black {
				if r >= MinRun {
					p += r + RunPDelta // RunP
				}
				black = !black
				r = 0
			}
			pat <<= 1
			if black {
				pat |= 1 << pShift
			} else if pat == FindB || pat == FindA {
				p += FindPP // FindP
			}
			r++
		}
		if r >= MinRun {
			p += r + RunPDelta // RunP
		}
		if pat <<= 1; pat == FindB {
			p += 2 * FindPP // 2×FindP
		} else {
			switch FindA {
			case pat, pat << 1, pat << 2, pat << 3:
				p += FindPP // FindP
			}
		}
	}
	return p
}

// A Mask describes a mask that is applied to the QR
// code to avoid QR artifacts being interpreted as
// alignment and timing patterns (such as the squares
// in the corners).  Valid masks are integers from 0 to 7.
type Mask int

// http://www.swetake.com/qr/qr5_en.html
var mfunc = []func(int, int) bool{
	func(i, j int) bool { return (i+j)%2 == 0 },
	func(i, j int) bool { return i%2 == 0 },
	func(i, j int) bool { return j%3 == 0 },
	func(i, j int) bool { return (i+j)%3 == 0 },
	func(i, j int) bool { return (i/2+j/3)%2 == 0 },
	func(i, j int) bool { return i*j%2+i*j%3 == 0 },
	func(i, j int) bool { return (i*j%2+i*j%3)%2 == 0 },
	func(i, j int) bool { return (i*j%3+(i+j)%2)%2 == 0 },
}

func (m Mask) Invert(y, x int) bool {
	if m < 0 {
		return false
	}
	return mfunc[m](y, x)
}

// A Plan describes how to construct a QR code
// with a specific version, level, and mask.
type Plan struct {
	Version Version
	Level   Level
	Mask    Mask

	DataBytes  int // number of data bytes
	CheckBytes int // number of error correcting (checksum) bytes
	Blocks     int // number of data blocks

	Pixel [][]Pixel // pixel map
	Code  Code      // 1 is black/inverted
}

// NewPlan returns a Plan for a QR code with the given
// version, level, and mask.
// If mask is -1, the Encode method will choose the best mask for the code.
func NewPlan(version Version, level Level, mask Mask) (*Plan, error) {
	if version < MinVersion || version > MaxVersion {
		return nil, fmt.Errorf("invalid QR version %d", int(version))
	}
	if level < L || level > H {
		return nil, fmt.Errorf("invalid QR level %d", int(level))
	}
	n := 1
	if mask == -1 {
		n = 8
	} else if mask < 0 || 7 < mask {
		return nil, fmt.Errorf("invalid QR mask %d", int(mask))
	}
	p, err := vplan(version, n)
	if err != nil {
		return nil, err
	}
	lplan(version, level, p)
	if n == 1 {
		fplan(level, mask, p, p.Code.Bitmap)
		mplan(mask, p, p.Code.Bitmap)
	} else {
		sz := p.Code.Size * p.Code.Stride
		for n := sz; n < len(p.Code.Bitmap); {
			n += copy(p.Code.Bitmap[n:], p.Code.Bitmap[:n])
		}
		for mask = 0; mask < 8; mask++ {
			fplan(level, mask, p, p.Code.Bitmap[int(mask)*sz:])
			mplan(mask, p, p.Code.Bitmap[int(mask)*sz:])
		}
		p.Mask = -1
	}
	return p, nil
}

// An AutoPlan describes how to construct a QR code with a
// specific version and level.
type AutoPlan struct {
	Version Version // Version
	Level   Level   // Error correction level
}

const (
	versions = MaxVersion - MinVersion + 1
	levels   = H - L + 1
)

type autoPlan struct {
	once sync.Once
	p    *Plan
}

var autoPlans [versions][levels]autoPlan

func makeAutoPlan(version Version, level Level) (*Plan, error) {
	if version < MinVersion || version > MaxVersion {
		return nil, fmt.Errorf("invalid QR version %d", int(version))
	}
	if level < L || level > H {
		return nil, fmt.Errorf("invalid QR level %d", int(level))
	}
	p := &autoPlans[version-MinVersion][level]
	if p.p == nil {
		p.once.Do(func() {
			p.p, _ = NewPlan(version, level, -1)
		})
	}
	return p.p, nil
}

// NewAutoPlan returns an AutoPlan for a QR code with the given
// version and level.  Its Encode method is functionally equivalent
// to that of a Plan returned by NewPlan(version, level, -1),
// except the Plan is permanently allocated.
func NewAutoPlan(version Version, level Level) (AutoPlan, error) {
	if _, err := makeAutoPlan(version, level); err != nil {
		return AutoPlan{}, err
	}
	return AutoPlan{version, level}, nil
}

func (b *Bits) Pad(n int) {
	if n < 0 {
		panic("qr: invalid pad size")
	}
	if n <= 4 {
		b.Write(0, n)
	} else {
		b.Write(0, 4)
		n -= 4
		n -= -b.Bits() & 7
		b.Write(0, -b.Bits()&7)
		pad := n / 8
		for i := 0; i < pad; i += 2 {
			b.Write(0xec, 8)
			if i+1 >= pad {
				break
			}
			b.Write(0x11, 8)
		}
	}
}

func (b *Bits) AddCheckBytes(v Version, l Level) {
	nd := v.DataBytes(l)
	if b.nbit < nd*8 {
		b.Pad(nd*8 - b.nbit)
	}
	if b.nbit != nd*8 {
		panic("qr: too much data")
	}

	dat := b.Bytes()
	vt := &vtab[v]
	lev := &vt.level[l]
	db := nd / lev.nblock
	extra := nd % lev.nblock
	chk := make([]byte, lev.check)
	rs := gf256.NewRSEncoder(Field, lev.check)
	for i := 0; i < lev.nblock; i++ {
		if i == lev.nblock-extra {
			db++
		}
		rs.ECC(dat[:db], chk)
		b.Append(chk)
		dat = dat[db:]
	}

	if len(b.Bytes()) != vt.bytes {
		panic("qr: internal error")
	}
}

func (p *Plan) Encode(text ...Encoding) (*Code, error) {
	var b Bits
	for _, t := range text {
		if err := t.Check(); err != nil {
			return nil, err
		}
		t.Encode(&b, p.Version)
	}
	if b.Bits() > p.DataBytes*8 {
		return nil, fmt.Errorf("cannot encode %d bits into %d-bit code",
			b.Bits(), p.DataBytes*8)
	}
	b.AddCheckBytes(p.Version, p.Level)
	bytes := b.Bytes()

	// Now we have the checksum bytes and the data bytes.
	// Construct the bitmap consisting of data and checksum bits.
	data := make([]byte, p.Code.Size*p.Code.Stride)
	if len(data) == len(p.Code.Bitmap) {
		copy(data, p.Code.Bitmap) // one mask: copy the bitmap
	}
	crow := data
	for _, row := range p.Pixel {
		for x, pix := range row {
			switch pix.Role() {
			case Data, Check:
				o := pix.Offset()
				if bytes[o/8]&(1<<uint(7-o&7)) != 0 {
					crow[x/8] ^= 1 << uint(7-x&7)
				}
			}
		}
		crow = crow[p.Code.Stride:]
	}

	c := &Code{Size: p.Code.Size, Stride: p.Code.Stride}
	if len(data) == len(p.Code.Bitmap) {
		c.Bitmap = data // one mask: done
	} else {
		// Apply masks to the bitmap to construct the actual codes.
		// Choose the code with the smallest penalty.
		c.Bitmap = make([]byte, len(data))
		best := make([]byte, len(data)) // best bitmap so far
		pen := 2 << 30                  // largest penalty is < 2<<23
		for b := p.Code.Bitmap; len(b) != 0; {
			// set bitmap to plan bits xor data bits
			b = b[copy(c.Bitmap, b):]
			for i, v := range data {
				c.Bitmap[i] ^= v
			}
			if p := c.Penalty(); p < pen {
				best, pen, c.Bitmap = c.Bitmap, p, best
			}
		}
		c.Bitmap = best
	}
	return c, nil
}

// Encode encodes text using p with 8 masks, returning the QR
// code with the smallest penalty.
func (a AutoPlan) Encode(text ...Encoding) (*Code, error) {
	p, err := makeAutoPlan(a.Version, a.Level)
	if err != nil {
		return nil, err
	}
	return p.Encode(text...)
}

// Encode encodes text using an AutoPlan with the given version and level.
func Encode(version Version, level Level, text ...Encoding) (*Code, error) {
	return AutoPlan{version, level}.Encode(text...)
}

// A version describes metadata associated with a version.
type version struct {
	apos    int
	astride int
	bytes   int
	pattern int
	level   [4]level
}

type level struct {
	nblock int
	check  int
}

var vtab = []version{
	{},
	{100, 100, 26, 0x0, [4]level{{1, 7}, {1, 10}, {1, 13}, {1, 17}}},          // 1
	{16, 100, 44, 0x0, [4]level{{1, 10}, {1, 16}, {1, 22}, {1, 28}}},          // 2
	{20, 100, 70, 0x0, [4]level{{1, 15}, {1, 26}, {2, 18}, {2, 22}}},          // 3
	{24, 100, 100, 0x0, [4]level{{1, 20}, {2, 18}, {2, 26}, {4, 16}}},         // 4
	{28, 100, 134, 0x0, [4]level{{1, 26}, {2, 24}, {4, 18}, {4, 22}}},         // 5
	{32, 100, 172, 0x0, [4]level{{2, 18}, {4, 16}, {4, 24}, {4, 28}}},         // 6
	{20, 16, 196, 0x7c94, [4]level{{2, 20}, {4, 18}, {6, 18}, {5, 26}}},       // 7
	{22, 18, 242, 0x85bc, [4]level{{2, 24}, {4, 22}, {6, 22}, {6, 26}}},       // 8
	{24, 20, 292, 0x9a99, [4]level{{2, 30}, {5, 22}, {8, 20}, {8, 24}}},       // 9
	{26, 22, 346, 0xa4d3, [4]level{{4, 18}, {5, 26}, {8, 24}, {8, 28}}},       // 10
	{28, 24, 404, 0xbbf6, [4]level{{4, 20}, {5, 30}, {8, 28}, {11, 24}}},      // 11
	{30, 26, 466, 0xc762, [4]level{{4, 24}, {8, 22}, {10, 26}, {11, 28}}},     // 12
	{32, 28, 532, 0xd847, [4]level{{4, 26}, {9, 22}, {12, 24}, {16, 22}}},     // 13
	{24, 20, 581, 0xe60d, [4]level{{4, 30}, {9, 24}, {16, 20}, {16, 24}}},     // 14
	{24, 22, 655, 0xf928, [4]level{{6, 22}, {10, 24}, {12, 30}, {18, 24}}},    // 15
	{24, 24, 733, 0x10b78, [4]level{{6, 24}, {10, 28}, {17, 24}, {16, 30}}},   // 16
	{28, 24, 815, 0x1145d, [4]level{{6, 28}, {11, 28}, {16, 28}, {19, 28}}},   // 17
	{28, 26, 901, 0x12a17, [4]level{{6, 30}, {13, 26}, {18, 28}, {21, 28}}},   // 18
	{28, 28, 991, 0x13532, [4]level{{7, 28}, {14, 26}, {21, 26}, {25, 26}}},   // 19
	{32, 28, 1085, 0x149a6, [4]level{{8, 28}, {16, 26}, {20, 30}, {25, 28}}},  // 20
	{26, 22, 1156, 0x15683, [4]level{{8, 28}, {17, 26}, {23, 28}, {25, 30}}},  // 21
	{24, 24, 1258, 0x168c9, [4]level{{9, 28}, {17, 28}, {23, 30}, {34, 24}}},  // 22
	{28, 24, 1364, 0x177ec, [4]level{{9, 30}, {18, 28}, {25, 30}, {30, 30}}},  // 23
	{26, 26, 1474, 0x18ec4, [4]level{{10, 30}, {20, 28}, {27, 30}, {32, 30}}}, // 24
	{30, 26, 1588, 0x191e1, [4]level{{12, 26}, {21, 28}, {29, 30}, {35, 30}}}, // 25
	{28, 28, 1706, 0x1afab, [4]level{{12, 28}, {23, 28}, {34, 28}, {37, 30}}}, // 26
	{32, 28, 1828, 0x1b08e, [4]level{{12, 30}, {25, 28}, {34, 30}, {40, 30}}}, // 27
	{24, 24, 1921, 0x1cc1a, [4]level{{13, 30}, {26, 28}, {35, 30}, {42, 30}}}, // 28
	{28, 24, 2051, 0x1d33f, [4]level{{14, 30}, {28, 28}, {38, 30}, {45, 30}}}, // 29
	{24, 26, 2185, 0x1ed75, [4]level{{15, 30}, {29, 28}, {40, 30}, {48, 30}}}, // 30
	{28, 26, 2323, 0x1f250, [4]level{{16, 30}, {31, 28}, {43, 30}, {51, 30}}}, // 31
	{32, 26, 2465, 0x209d5, [4]level{{17, 30}, {33, 28}, {45, 30}, {54, 30}}}, // 32
	{28, 28, 2611, 0x216f0, [4]level{{18, 30}, {35, 28}, {48, 30}, {57, 30}}}, // 33
	{32, 28, 2761, 0x228ba, [4]level{{19, 30}, {37, 28}, {51, 30}, {60, 30}}}, // 34
	{28, 24, 2876, 0x2379f, [4]level{{19, 30}, {38, 28}, {53, 30}, {63, 30}}}, // 35
	{22, 26, 3034, 0x24b0b, [4]level{{20, 30}, {40, 28}, {56, 30}, {66, 30}}}, // 36
	{26, 26, 3196, 0x2542e, [4]level{{21, 30}, {43, 28}, {59, 30}, {70, 30}}}, // 37
	{30, 26, 3362, 0x26a64, [4]level{{22, 30}, {45, 28}, {62, 30}, {74, 30}}}, // 38
	{24, 28, 3532, 0x27541, [4]level{{24, 30}, {47, 28}, {65, 30}, {77, 30}}}, // 39
	{28, 28, 3706, 0x28c69, [4]level{{25, 30}, {49, 28}, {68, 30}, {81, 30}}}, // 40
}

func grid(siz int) [][]Pixel {
	m := make([][]Pixel, siz)
	pix := make([]Pixel, siz*siz)
	for i := range m {
		m[i], pix = pix[:siz], pix[siz:]
	}
	return m
}

// vplan creates a Plan for the given version.
func vplan(v Version, n int) (*Plan, error) {
	p := &Plan{Version: v}
	if v < 1 || v > 40 {
		return nil, fmt.Errorf("invalid QR version %d", int(v))
	}
	siz := 17 + int(v)*4
	m := grid(siz)
	p.Pixel = m
	p.Code.Size = siz
	p.Code.Stride = (siz + 7) >> 3
	p.Code.Bitmap = make([]byte, p.Code.Stride*siz*n)

	// Timing markers (overwritten by boxes).
	const ti = 6 // timing is in row/column 6 (counting from 0)
	pix := Timing.Pixel()
	for i := range m {
		m[i][ti] = pix
		m[ti][i] = pix
		if i&1 == 0 {
			p.Code.set(p.Code.Bitmap, i, ti)
			p.Code.set(p.Code.Bitmap, ti, i)
		}
	}

	// Position boxes.
	posBox(m, &p.Code, 0, 0)
	posBox(m, &p.Code, siz-7, 0)
	posBox(m, &p.Code, 0, siz-7)

	// Alignment boxes.
	info := &vtab[v]
	for x := 4; x+5 < siz; {
		for y := 4; y+5 < siz; {
			// don't overwrite timing markers
			if (x < 7 && y < 7) || (x < 7 && y+5 >= siz-7) || (x+5 >= siz-7 && y < 7) {
			} else {
				alignBox(m, &p.Code, x, y)
			}
			if y == 4 {
				y = info.apos
			} else {
				y += info.astride
			}
		}
		if x == 4 {
			x = info.apos
		} else {
			x += info.astride
		}
	}

	// Version pattern.
	pat := vtab[v].pattern
	if pat != 0 {
		pix := PVersion.Pixel()
		v := pat
		for x := 0; x < 6; x++ {
			for y := 0; y < 3; y++ {
				m[siz-11+y][x] = pix
				m[x][siz-11+y] = pix
				if v&1 != 0 {
					p.Code.set(p.Code.Bitmap, siz-11+y, x)
					p.Code.set(p.Code.Bitmap, x, siz-11+y)
				}
				v >>= 1
			}
		}
	}

	// Format pixels.
	for i := uint(0); i < 15; i++ {
		pix := Format.Pixel() + OffsetPixel(i)
		switch {
		case i < 6:
			p.Pixel[i][8] = pix
		case i < 8:
			p.Pixel[i+1][8] = pix
		case i < 9:
			p.Pixel[8][7] = pix
		default:
			p.Pixel[8][14-i] = pix
		}
		// bottom right
		switch {
		case i < 8:
			p.Pixel[8][siz-1-int(i)] = pix
		default:
			p.Pixel[siz-1-int(14-i)][8] = pix
		}
	}

	// One lonely black pixel
	m[siz-8][8] = Unused.Pixel()
	p.Code.set(p.Code.Bitmap, siz-8, 8)

	return p, nil
}

// fplan sets the format bits
func fplan(l Level, m Mask, p *Plan, b []byte) error {
	// Format pixels.
	fb := uint32(l^1) << 13 // level: L=01, M=00, Q=11, H=10
	fb |= uint32(m) << 10   // mask
	const formatPoly = 0x537
	rem := fb
	for i := 14; i >= 10; i-- {
		if rem&(1<<uint(i)) != 0 {
			rem ^= formatPoly << uint(i-10)
		}
	}
	fb |= rem
	fb ^= uint32(0x5412)
	siz := len(p.Pixel)
	for i := 0; i < 15; i++ {
		if (fb>>i)&1 == 1 {
			switch {
			case i < 6:
				p.Code.set(b, i, 8)
			case i < 8:
				p.Code.set(b, i+1, 8)
			case i < 9:
				p.Code.set(b, 8, 7)
			default:
				p.Code.set(b, 8, 14-i)
			}
			// bottom right
			switch {
			case i < 8:
				p.Code.set(b, 8, siz-1-i)
			default:
				p.Code.set(b, siz-1-14+i, 8)
			}
		}
	}
	return nil
}

// lplan edits a version-only Plan to add information
// about the error correction levels.
func lplan(v Version, l Level, p *Plan) error {
	p.Level = l

	nblock := vtab[v].level[l].nblock
	ne := vtab[v].level[l].check
	nde := (vtab[v].bytes - ne*nblock) / nblock
	extra := (vtab[v].bytes - ne*nblock) % nblock
	dataBits := (nde*nblock + extra) * 8
	checkBits := ne * nblock * 8

	p.DataBytes = vtab[v].bytes - ne*nblock
	p.CheckBytes = ne * nblock
	p.Blocks = nblock

	// Make data + checksum pixels.
	data := make([]Pixel, dataBits)
	for i := range data {
		data[i] = Data.Pixel() | OffsetPixel(uint(i))
	}
	check := make([]Pixel, checkBits)
	for i := range check {
		check[i] = Check.Pixel() | OffsetPixel(uint(i+dataBits))
	}

	// Split into blocks.
	dataList := make([][]Pixel, nblock)
	checkList := make([][]Pixel, nblock)
	for i := 0; i < nblock; i++ {
		// The last few blocks have an extra data byte (8 pixels).
		nd := nde
		if i >= nblock-extra {
			nd++
		}
		dataList[i], data = data[0:nd*8], data[nd*8:]
		checkList[i], check = check[0:ne*8], check[ne*8:]
	}
	if len(data) != 0 || len(check) != 0 {
		panic("data/check math")
	}

	// Build up bit sequence, taking first byte of each block,
	// then second byte, and so on.  Then checksums.
	bits := make([]Pixel, dataBits+checkBits)
	dst := bits
	for i := 0; i < nde+1; i++ {
		for _, b := range dataList {
			if i*8 < len(b) {
				copy(dst, b[i*8:(i+1)*8])
				dst = dst[8:]
			}
		}
	}
	for i := 0; i < ne; i++ {
		for _, b := range checkList {
			if i*8 < len(b) {
				copy(dst, b[i*8:(i+1)*8])
				dst = dst[8:]
			}
		}
	}
	if len(dst) != 0 {
		panic("dst math")
	}

	// Sweep up pair of columns,
	// then down, assigning to right then left pixel.
	// Repeat.
	// See Figure 2 of http://www.pclviewer.com/rs2/qrtopology.htm
	siz := len(p.Pixel)
	rem := make([]Pixel, 7)
	for i := range rem {
		rem[i] = Extra.Pixel()
	}
	src := append(bits, rem...)
	for x := siz; x > 0; {
		for y := siz - 1; y >= 0; y-- {
			if p.Pixel[y][x-1].Role() == 0 {
				p.Pixel[y][x-1], src = src[0], src[1:]
			}
			if p.Pixel[y][x-2].Role() == 0 {
				p.Pixel[y][x-2], src = src[0], src[1:]
			}
		}
		x -= 2
		if x == 7 { // vertical timing strip
			x--
		}
		for y := 0; y < siz; y++ {
			if p.Pixel[y][x-1].Role() == 0 {
				p.Pixel[y][x-1], src = src[0], src[1:]
			}
			if p.Pixel[y][x-2].Role() == 0 {
				p.Pixel[y][x-2], src = src[0], src[1:]
			}
		}
		x -= 2
	}
	return nil
}

// mplan edits a version+level-only Plan to add the mask.
func mplan(m Mask, p *Plan, b []byte) error {
	p.Mask = m
	for y, row := range p.Pixel {
		for x, pix := range row {
			if r := pix.Role(); (r == Data || r == Check || r == Extra) && p.Mask.Invert(y, x) {
				p.Code.set(b, y, x)
			}
		}
	}
	return nil
}

// posBox draws a position (large) box at upper left x, y.
func posBox(m [][]Pixel, c *Code, x, y int) {
	pos := Position.Pixel()
	// box
	for dy := 0; dy < 7; dy++ {
		for dx := 0; dx < 7; dx++ {
			m[y+dy][x+dx] = pos
			if dx == 0 || dx == 6 || dy == 0 || dy == 6 || 2 <= dx && dx <= 4 && 2 <= dy && dy <= 4 {
				c.set(c.Bitmap, y+dy, x+dx)
			}
		}
	}
	// white border
	for dy := -1; dy < 8; dy++ {
		if 0 <= y+dy && y+dy < len(m) {
			if x > 0 {
				m[y+dy][x-1] = pos
			}
			if x+7 < len(m) {
				m[y+dy][x+7] = pos
			}
		}
	}
	for dx := -1; dx < 8; dx++ {
		if 0 <= x+dx && x+dx < len(m) {
			if y > 0 {
				m[y-1][x+dx] = pos
			}
			if y+7 < len(m) {
				m[y+7][x+dx] = pos
			}
		}
	}
}

// alignBox draw an alignment (small) box at upper left x, y.
func alignBox(m [][]Pixel, c *Code, x, y int) {
	// box
	align := Alignment.Pixel()
	for dy := 0; dy < 5; dy++ {
		for dx := 0; dx < 5; dx++ {
			m[y+dy][x+dx] = align
			if dx == 0 || dx == 4 || dy == 0 || dy == 4 || dx == 2 && dy == 2 {
				c.set(c.Bitmap, y+dy, x+dx)
			}
		}
	}
}
