package main

import (
	"encoding/binary"
	"fmt"
	"github.com/dgraph-io/badger/v2"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

const (
	beforeIncTrials = 50
	maxTrials       = 80
	lengthKey       = "settings:last_length"
	urlPrefix       = "url:"
	counterPrefix   = "hits:overall:"
	weekLogPrefix   = "hits:weeklog:"
	serverAddress   = "http://localhost:8077"
)

var (
	genLocker     sync.RWMutex
	currentLength uint64
	db            *badger.DB
	letterRunes   = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")
)

func main() {
	currentLength = 4

	var err error
	db, err = badger.Open(badger.DefaultOptions("/tmp/gouss_badger"))
	if err != nil {
		log.Fatal(errors.Wrap(err, "couldn't open database"))
	}
	defer db.Close()

	if err := GetSettings(); err != nil {
		log.Fatal(errors.Wrap(err, "couldn't get settings"))
	}

	r := mux.NewRouter()

	r.HandleFunc("/", IndexPage)
	r.HandleFunc("/set", SetUrl).Methods("POST")
	r.HandleFunc("/{url:[-_A-Za-z0-9][-_A-Za-z0-9][-_A-Za-z0-9][-_A-Za-z0-9]+}", Expander).
		Methods("GET")
	r.HandleFunc("/{url:[-_A-Za-z0-9][-_A-Za-z0-9][-_A-Za-z0-9][-_A-Za-z0-9]+}/stat", URLStat).
		Methods("GET")

	http.Handle("/", r)
	err = http.ListenAndServe(":8077", nil)
	log.Fatal(errors.Wrap(err, "server listen and serve failed"))
}

func GetUInt64(txn *badger.Txn, key string, _default uint64) (uint64, error) {
	item, err := txn.Get([]byte(key))
	if err == badger.ErrKeyNotFound {
		return _default, nil
	}
	if err != nil {
		return _default, errors.Wrap(err, "failed to get item from db")
	}
	var ret = _default
	if err = item.Value(func(val []byte) error {
		ret = binary.BigEndian.Uint64(val)
		return nil
	}); err != nil {
		return _default, errors.Wrap(err, "failed to convert item to uint64")
	}
	return ret, nil
}

func GetString(txn *badger.Txn, key string, _default string) (string, error) {
	item, err := txn.Get([]byte(key))
	if err == badger.ErrKeyNotFound {
		return _default, nil
	}
	if err != nil {
		return _default, errors.Wrap(err, "failed to get item from db")
	}
	var ret = _default
	if err = item.Value(func(val []byte) error {
		ret = string(val)
		return nil
	}); err != nil {
		return _default, errors.Wrap(err, "failed to convert item to string")
	}
	return ret, nil
}

func SetUint64(txn *badger.Txn, key string, val uint64) error {
	return txn.Set([]byte(key), uint64ToBytes(val))
}

func SetString(txn *badger.Txn, key string, val string) error {
	return txn.Set([]byte(key), []byte(val))
}

func GetSettings() error {
	return db.View(func(txn *badger.Txn) error {
		l, err := GetUInt64(txn, lengthKey, currentLength)
		if err != nil {
			return errors.Wrap(err, "length variable get failed")
		}
		currentLength = l
		return nil
	})
}

func GenRandomURL() string {
	b := make([]rune, currentLength)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func IndexPage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
Usage:
%s/ -- this page
%s/set -- POST URL to shorten, answer will be shortened URL
    Payload (plain text):
        http(s)://your.url/you/whant/to/shorten
    Answer (plain text):
        %s/ShtndURL
%s/<shortened_URL> -- expand the URL
%s/<shortened_URL>/stat -- get stats for the URL
`, serverAddress, serverAddress, serverAddress, serverAddress, serverAddress)
}

func FailHTTP(err error, w http.ResponseWriter) {
	fmt.Printf("Failed during HTTP request: %v", err)
	w.WriteHeader(500)
	w.Write([]byte("Server failure"))
}

func uint64ToBytes(i uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], i)
	return buf[:]
}

func SetUrl(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		FailHTTP(errors.Wrap(err, "failed to get body"), w)
		return
	}
	// TestURL(body)
	var shortener string
	if err := db.Update(func(txn *badger.Txn) error {
		genLocker.RLock()
		defer genLocker.RUnlock()

		for iteration := 1; iteration <= maxTrials; iteration++ {
			fmt.Printf("Iteration: %d\n", iteration)
			if iteration%beforeIncTrials == 0 {
				currentLength++

				if err := SetUint64(txn, lengthKey, currentLength); err != nil {
					return errors.Wrap(err, "couldn't save length")
				}
			}
			shortener = GenRandomURL()
			urlKey := urlPrefix + shortener

			str, err := GetString(txn, urlKey, "")
			if err != nil {
				return errors.Wrap(err, "couldn't query shortener for existence")
			}
			if str == "" {
				if err := SetString(txn, urlKey, string(body)); err != nil {
					return errors.Wrap(err, "couldn't save shortener")
				}
				return nil
			}
		}
		return errors.New("failed to generate shortener")
	}); err != nil {
		FailHTTP(errors.Wrap(err, "failed to save URL"), w)
	} else if _, err := w.Write([]byte(serverAddress + "/" + shortener)); err != nil {
		FailHTTP(errors.Wrap(err, "failed to output shortener"), w)
	}
}

func IncrementCounter(txn *badger.Txn, counterKey string) error {
	counter, err := GetUInt64(txn, counterKey, 0)
	if err != nil {
		return errors.Wrap(err, "failed to get counter")
	}
	counter++
	if err := SetUint64(txn, counterKey, counter); err != nil {
		return errors.Wrap(err, "failed to save counter")
	}
	return nil
}

func GetUInt64Array(txn *badger.Txn, key string) ([]uint64, error) {
	item, err := txn.Get([]byte(key))
	if err == badger.ErrKeyNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get item from db")
	}
	var ret []uint64
	if err = item.Value(func(val []byte) error {
		ret = make([]uint64, 0, len(val)/8)
		for i := 0; i < len(val); i += 8 {
			ret = append(ret, binary.BigEndian.Uint64(val[i:i+8]))
		}
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to convert item to uint64 array")
	}
	return ret, nil
}

func SetUInt64Array(txn *badger.Txn, key string, arr []uint64) error {
	bytes := make([]byte, 0, len(arr)*8)
	for _, el := range arr {
		bytes = append(bytes, uint64ToBytes(el)...)
	}
	return txn.Set([]byte(key), bytes)
}

func IncrementWeekLog(txn *badger.Txn, weekLogKey string) error {
	weekLog, err := GetUInt64Array(txn, weekLogKey)
	if err != nil {
		return errors.Wrap(err, "couldn't get week log")
	}
	now := time.Now()
	weekLog = append(weekLog, uint64(now.UnixNano()))
	weekAgo := uint64(now.Add(-7 * 24 * time.Hour).UnixNano()) // to the hell with leap seconds and shit
	cleanedWeekLog := make([]uint64, 0)
	for _, ts := range weekLog {
		if ts > weekAgo {
			cleanedWeekLog = append(cleanedWeekLog, ts)
		}
	}
	if err := SetUInt64Array(txn, weekLogKey, cleanedWeekLog); err != nil {
		return errors.Wrap(err, "couldn't save week log")
	}

	return nil
}

func URLStat(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	shortener := vars["url"]
	var redirectTarget string
	var counter uint64
	var weekCounter uint64
	var dayCounter uint64
	if err := db.View(func(txn *badger.Txn) error {
		targetItem, err := GetString(txn, urlPrefix+shortener, "")
		if err != nil {
			return errors.Wrap(err, "failed to get shortened url")
		}
		if targetItem == "" {
			w.WriteHeader(404)
			_, err := w.Write([]byte("URL not found"))
			if err != nil {
				return errors.Wrap(err, "failed to write 404 answer")
			}
			return nil
		}
		redirectTarget = targetItem
		counter, err = GetUInt64(txn, counterPrefix+shortener, 0)
		if err != nil {
			return errors.Wrap(err, "failed to get counter")
		}
		weekLog, err := GetUInt64Array(txn, weekLogPrefix+shortener)
		if err != nil {
			return errors.Wrap(err, "failed to get week log")
		}
		now := time.Now()
		weekAgo := uint64(now.Add(-7 * 24 * time.Hour).UnixNano()) // to the hell with leap seconds and shit
		dayAgo := uint64(now.Add(-24 * time.Hour).UnixNano())      // to the hell with leap seconds and shit

		for _, ts := range weekLog {
			if ts > weekAgo {
				weekCounter++
			}
			if ts > dayAgo {
				dayCounter++
			}
		}

		return nil
	}); err != nil {
		FailHTTP(errors.Wrap(err, "failed to get URL and stats"), w)
		return
	}
	_, err := fmt.Fprintf(w, `
Shortened URL: %s/%s<br>
Real URL: %s<br>
Overall hits: %d<br>
Weekly hits: %d<br>
24h hits: %d<br>
`, serverAddress, shortener, redirectTarget, counter, weekCounter, dayCounter)
	if err != nil {
		FailHTTP(errors.Wrap(err, "failed to output results"), w)
		return
	}
}

func Incrementer(shortener string, targetItem string) {
	if err := db.Update(func(txn *badger.Txn) error {
		if err := IncrementCounter(txn, counterPrefix+shortener); err != nil {
			return errors.Wrap(err, "failed to increment counter")
		}
		if err := IncrementWeekLog(txn, weekLogPrefix+shortener); err != nil {
			return errors.Wrap(err, "failed to increment week log")
		}
		return nil
	}); err != nil {
		fmt.Printf("Failed during updating the stats: %v", err)
	}
}

func Expander(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	shortener := vars["url"]
	if err := db.View(func(txn *badger.Txn) error {
		targetItem, err := GetString(txn, urlPrefix+shortener, "")
		if err != nil {
			return errors.Wrap(err, "failed to get shortened url")
		}
		if targetItem == "" {
			w.WriteHeader(404)
			_, err := w.Write([]byte("URL not found"))
			if err != nil {
				return errors.Wrap(err, "failed to write 404 answer")
			}
			return nil
		} else {
			http.Redirect(w, r, targetItem, 308)
		}
		go Incrementer(shortener, targetItem)
		return nil
	}); err != nil {
		FailHTTP(errors.Wrap(err, "failed to get URL"), w)
		return
	}
}
