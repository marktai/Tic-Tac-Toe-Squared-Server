package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"db"
	"encoding/base64"
	"encoding/hex"
	"errors"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
	"math/big"
	//	mrand "math/rand"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var secretMap = make(map[uint]*Secret)

const BADCHARS = "[^a-zA-Z0-9_]"

type Secret struct {
	value      *big.Int
	expiration time.Time
}

// secrets are an arbitrary big int number from 0 to 2^512
// to actually use their value, they are converted into base64
// then the base64 string chararcters are used as bytes
// this is to get random bytes and still be able to nicely store them in strings
func (s *Secret) Bytes() []byte {
	return s.value.Bytes()
}

func (s *Secret) Base64() string {
	return base64.StdEncoding.EncodeToString(s.Bytes())
}

func (s *Secret) String() string {
	return s.Base64()
}

func (s *Secret) Expired() bool {
	return time.Now().After(s.expiration)
}

func (s *Secret) ExpirationUTC() string {
	return s.expiration.UTC().Format(time.RFC3339)
}

func (s *Secret) resetExpiration() {
	s.expiration = time.Now().Add(30 * time.Minute)
}

var bitSize int64 = 512

var limit *big.Int

func newSecret() (*Secret, error) {
	if limit == nil {
		limit = big.NewInt(2)
		limit.Exp(big.NewInt(2), big.NewInt(bitSize), nil)
	}

	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	retSecret := &Secret{}
	retSecret.value = value
	retSecret.resetExpiration()
	return retSecret, nil
}

func stringtoUint(s string) (uint, error) {
	i, err := strconv.Atoi(s)
	return uint(i), err
}

// checks if user id conflict in database
func checkIDConflict(id uint) (bool, error) {
	collision := 1
	err := db.Db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE userid=?)", id).Scan(&collision)
	return collision != 0, err
}

// returns a unique id for a user
func getUniqueID() (uint, error) {

	var count uint
	var scale uint
	var addConst uint

	var newID uint

	err := db.Db.QueryRow("SELECT count, scale, addConst FROM count WHERE type='users'").Scan(&count, &scale, &addConst)
	if err != nil {
		return 0, err
	}

	conflict := true
	for conflict || newID == 0 {
		count += 1
		newID = (count*scale + addConst) % 65536

		conflict, err = checkIDConflict(newID)
		if err != nil {
			return 0, err
		}
	}

	updateCount, err := db.Db.Prepare("UPDATE count SET count=? WHERE type='users'")
	if err != nil {
		return newID, err
	}

	_, err = updateCount.Exec(count)
	if err != nil {
		return newID, err
	}

	return newID, nil
}

// returns the userid of a user
func GetUserID(user string) (uint, error) {
	var userID uint
	if bad, err := regexp.MatchString(BADCHARS, user); err != nil {
		return 0, err
	} else if bad {
		return 0, errors.New("Invalid user name")
	}
	err := db.Db.QueryRow("SELECT userid FROM users WHERE name=?", user).Scan(&userID)
	if err != nil {
		return 0, err
	}

	return userID, nil
}

// returns the username of a user
func GetUsername(userID uint) (string, error) {
	var username string

	err := db.Db.QueryRow("SELECT name FROM users WHERE userid=?", userID).Scan(&username)
	if err != nil {
		return "", err
	}

	return username, nil
}

// makes a new user and returns its id
func MakeUser(user, pass string) (uint, error) {

	// username santization in GetUserID
	userID, err := GetUserID(user)
	if err != nil && err.Error() != "sql: no rows in result set" {
		// if there is an error
		// does not include error when user is not found
		return 0, err
	} else if userID != 0 {
		return 0, errors.New("User already made")
	}

	id, err := getUniqueID()
	if err != nil {
		return 0, err
	}

	saltHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	saltHashString := base64.StdEncoding.EncodeToString(saltHash)

	err = db.Db.Ping()
	if err != nil {
		return 0, err
	}

	addUser, err := db.Db.Prepare("INSERT INTO users (userid, name, salthash) VALUES(?, ?, ?)")
	if err != nil {
		return 0, err
	}

	_, err = addUser.Exec(id, user, saltHashString)

	if err != nil {
		return 0, err
	}

	return id, nil

}

// returns the salthash of a user
func getSaltHash(userID uint) ([]byte, error) {
	saltHashString := ""
	err := db.Db.QueryRow("SELECT salthash FROM users WHERE userid=?", userID).Scan(&saltHashString)
	if err != nil {
		return nil, err
	}
	saltHash, err := base64.StdEncoding.DecodeString(saltHashString)
	return saltHash, err
}

// if a successful login, generates a secret or refreshes the existing one
func Login(user, pass string) (uint, *Secret, error) {

	// username santization in GetUserID
	userID, err := GetUserID(user)
	if err != nil {
		return 0, nil, err
	}

	hash, err := getSaltHash(userID)
	if err != nil {
		return 0, nil, err
	}

	err = bcrypt.CompareHashAndPassword(hash, []byte(pass))

	if err != nil {
		return 0, nil, err
	}

	if _, ok := secretMap[userID]; !ok || secretMap[userID].Expired() {
		secret, err := newSecret()
		if err != nil {
			return 0, nil, err
		}
		secretMap[userID] = secret
	}

	secretMap[userID].resetExpiration()

	return userID, secretMap[userID], nil
}

// if the user and secret are correct, refreshes the secret
func VerifySecret(user, inpSecret string) (uint, *Secret, error) {

	// username santization in GetUserID
	userID, err := GetUserID(user)
	if err != nil {
		return 0, nil, err
	}

	if _, ok := secretMap[userID]; !ok {
		return 0, nil, errors.New("No secret found for user")
	} else if secretMap[userID].Expired() {
		return 0, nil, errors.New("Secret has expired")
	} else if secretMap[userID].String() != inpSecret {
		return 0, nil, errors.New("Secrets do not match")
	}

	secretMap[userID].resetExpiration()

	return userID, secretMap[userID], nil
}

func ComputeHmac256(message, key string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	expectedMAC := mac.Sum(nil)

	return expectedMAC
}

// CheckMAC reports whether messageHMAC is a valid HMAC tag for message.
func checkMAC(key, message string, messageHMAC []byte) bool {
	expectedMAC := ComputeHmac256(message, key)
	equal := hmac.Equal(messageHMAC, expectedMAC)
	if !equal {
		log.Println("key:", key, "\nmessage:", message, "\nexpected:", expectedMAC, "\nreceived:", messageHMAC)
	}

	return equal
}

// returns userID, message used to generate HMAC, and HMAC from request
func parseRequest(r *http.Request) (string, string, []byte, error) {
	timeSlice, ok := r.Header["Time-Sent"]
	if !ok || timeSlice == nil || len(timeSlice) == 0 {
		return "", "", nil, errors.New("No Time-Sent header provided")
	}

	time := timeSlice[0]

	message := time + ":" + r.URL.String()

	messageHMACSlice, ok := r.Header["Hmac"]
	if !ok || messageHMACSlice == nil || len(messageHMACSlice) == 0 {
		return "", "", nil, errors.New("No HMAC header provided")
	}

	messageHMACString := messageHMACSlice[0]
	HMACEncoding := ""

	encodingSlice, ok := r.Header["Encoding"]
	if ok && encodingSlice != nil {
		encoding := encodingSlice[0]
		if ok {
			switch strings.ToLower(encoding) {
			case "base64", "64":
				HMACEncoding = "base64"
			case "hex", "hexadecimal":
				HMACEncoding = "hex"
			case "binary", "bits":
				HMACEncoding = "binary"
			case "decimal":
				HMACEncoding = "decimal"
			default:
				HMACEncoding = encoding
			}
		}
	} else {
		HMACEncoding = "hex"
	}

	var messageHMAC []byte
	var err error
	switch HMACEncoding {
	case "base64":
		messageHMAC, err = base64.StdEncoding.DecodeString(messageHMACString)
	case "hex":
		messageHMAC, err = hex.DecodeString(messageHMACString)
	default:
		return "", "", nil, errors.New("'" + HMACEncoding + "' not a supported encoding")
	}

	if err != nil {
		return "", "", nil, err
	}

	return message, time, messageHMAC, nil
}

// Check README.md for documentation
// Request Headers
// HMAC - encoded HMAC with SHA 256
// Encoding - encoding format (default hex)
// Time-Sent - seconds since epoch

// Verifies whether a request is correctly authorized
func AuthRequest(r *http.Request, userID uint) (bool, error) {
	message, timeString, messageHMAC, err := parseRequest(r)
	if err != nil {
		return false, err
	}

	timeInt, err := strconv.Atoi(timeString)
	if err != nil {
		return false, errors.New("Error parsing time (seconds since epoch)")
	}

	delay := int64(timeInt) - time.Now().Unix()

	// rejects if times are more than 30 seconds apart
	// used to be 10, but sometimes had authentication rejects because of it
	if delay < -30 || delay > 30 {
		return false, errors.New("Time difference too large")
	}

	secret, ok := secretMap[userID]
	if !ok {
		return false, errors.New("No secret for that user")
	}

	if secret.Expired() {
		return false, errors.New("Secret expired")
	}

	secretString := secret.String()
	return checkMAC(secretString, message, messageHMAC), nil
}
