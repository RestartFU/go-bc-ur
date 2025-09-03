package bcur

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"log"
	"strings"
	"unicode"

	"github.com/fxamacker/cbor/v2"
)

var bytewords = "ableacidalsoapexaquaarchatomauntawayaxisbackbaldbarnbeltbetabiasbluebodybragbrewbulbbuzzcalmcashcatschefcityclawcodecolacookcostcruxcurlcuspcyandarkdatadaysdelidicedietdoordowndrawdropdrumdulldutyeacheasyechoedgeepicevenexamexiteyesfactfairfernfigsfilmfishfizzflapflewfluxfoxyfreefrogfuelfundgalagamegeargemsgiftgirlglowgoodgraygrimgurugushgyrohalfhanghardhawkheathelphighhillholyhopehornhutsicedideaidleinchinkyintoirisironitemjadejazzjoinjoltjowljudojugsjumpjunkjurykeepkenokeptkeyskickkilnkingkitekiwiknoblamblavalazyleaflegsliarlimplionlistlogoloudloveluaulucklungmainmanymathmazememomenumeowmildmintmissmonknailnavyneednewsnextnoonnotenumbobeyoboeomitonyxopenovalowlspaidpartpeckplaypluspoempoolposepuffpumapurrquadquizraceramprealredorichroadrockroofrubyruinrunsrustsafesagascarsetssilkskewslotsoapsolosongstubsurfswantacotasktaxitenttiedtimetinytoiltombtoystriptunatwinuglyundouniturgeuservastveryvetovialvibeviewvisavoidvowswallwandwarmwaspwavewaxywebswhatwhenwhizwolfworkyankyawnyellyogayurtzapszerozestzinczonezoom"

var words [256]string
var lookupTable [26 * 26]int16

func init() {
	// build word table
	for i := 0; i < 256; i++ {
		words[i] = bytewords[i*4 : i*4+4]
	}

	// init lookup
	for i := range lookupTable {
		lookupTable[i] = -1
	}
	for i, w := range words {
		x := w[0] - 'a'
		y := w[3] - 'a'
		offset := int(y)*26 + int(x)
		lookupTable[offset] = int16(i)
	}
}

func Decode(input string) Root {
	// Step 1: Bytewords decode
	decoded, err := decode(input, 2, "")
	if err != nil {
		log.Fatal("Bytewords decode failed:", err)
	}

	// Step 2: outer CBOR
	var outer []byte
	if err := cbor.Unmarshal(decoded, &outer); err != nil {
		log.Fatal("CBOR outer decode failed:", err)
	}

	// Step 3: gunzip
	gr, err := gzip.NewReader(bytes.NewReader(outer))
	if err != nil {
		log.Fatal("gzip reader failed:", err)
	}
	unzipped, err := io.ReadAll(gr)
	gr.Close()
	if err != nil {
		log.Fatal("gzip read failed:", err)
	}

	// Step 4: decode inner CBOR as []any
	var inner any
	if err := cbor.Unmarshal(unzipped, &inner); err != nil {
		log.Fatal("Inner CBOR decode failed:", err)
	}

	j, _ := json.MarshalIndent(inner, "", "  ")

	var raw []any
	if err := json.Unmarshal([]byte(j), &raw); err != nil {
		panic(err)
	}

	version := int(raw[0].(float64))
	accList := raw[1].([]any)

	var accounts []Account
	for _, a := range accList {
		arr := a.([]any)
		w := arr[4].([]any)
		account := Account{
			ID:    int(arr[0].(float64)),
			Index: int(arr[1].(float64)),
			Type:  arr[2].(string),
			Block: int(arr[3].(float64)),
			Wallet: WalletInfo{
				DerivationPath: w[0].(string),
				ChainCode:      w[1].(string),
				Name:           w[2].(string),
				Internal1:      w[3].(bool),
				Internal2:      w[4].(bool),
				SomeBytes:      w[5].(string),
				XPub:           w[6].(string),
			},
		}
		accounts = append(accounts, account)
	}

	return Root{
		Version:  version,
		Accounts: accounts,
	}
}

// decode a single word (len=2 for minimal, 4 for full)
func decodeWord(word string, wordLen int) (byte, error) {
	if len(word) != wordLen {
		return 0, errors.New("invalid bytewords length")
	}
	x := unicode.ToLower(rune(word[0])) - 'a'
	var y rune
	if wordLen == 4 {
		y = unicode.ToLower(rune(word[3])) - 'a'
	} else {
		y = unicode.ToLower(rune(word[1])) - 'a'
	}
	if x < 0 || x >= 26 || y < 0 || y >= 26 {
		return 0, errors.New("invalid bytewords")
	}
	offset := int(y)*26 + int(x)
	val := lookupTable[offset]
	if val == -1 {
		return 0, errors.New("invalid bytewords")
	}
	if wordLen == 4 {
		expected := words[val]
		if word[1] != expected[1] || word[2] != expected[2] {
			return 0, errors.New("invalid bytewords middle chars")
		}
	}
	return byte(val), nil
}

// CRC32 util
func crc32Bytes(data []byte) []byte {
	crc := crc32.ChecksumIEEE(data)
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, crc)
	return out
}

func appendCRC(data []byte) []byte {
	return append(data, crc32Bytes(data)...)
}

// Encode minimal (2-char per byte)
func encodeMinimal(data []byte) string {
	data = appendCRC(data)
	var sb strings.Builder
	for _, b := range data {
		w := words[b]
		sb.WriteByte(w[0])
		sb.WriteByte(w[3])
	}
	return sb.String()
}

// Encode standard (4-char per byte, sep by space)
func encodeStandard(data []byte) string {
	data = appendCRC(data)
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = words[b]
	}
	return strings.Join(parts, " ")
}

// Decode
func decode(s string, wordLen int, sep string) ([]byte, error) {
	var tokens []string
	if wordLen == 4 {
		tokens = strings.Split(s, sep)
	} else {
		// minimal → 2-char chunks
		for i := 0; i < len(s); i += 2 {
			tokens = append(tokens, s[i:i+2])
		}
	}

	buf := make([]byte, len(tokens))
	for i, t := range tokens {
		b, err := decodeWord(t, wordLen)
		if err != nil {
			return nil, err
		}
		buf[i] = b
	}
	if len(buf) < 5 {
		return nil, errors.New("too short")
	}
	body := buf[:len(buf)-4]
	checksum := buf[len(buf)-4:]
	if !equal(checksum, crc32Bytes(body)) {
		return nil, errors.New("crc mismatch")
	}
	return body, nil
}

func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type WalletInfo struct {
	DerivationPath string
	ChainCode      string
	Name           string
	Internal1      bool
	Internal2      bool
	SomeBytes      string
	XPub           string
}

type Account struct {
	ID     int
	Index  int
	Type   string
	Block  int
	Wallet WalletInfo
}

// Because the outer structure is an array-of-array, we need to use a custom type
type Root struct {
	Version  int
	Accounts []Account
}
