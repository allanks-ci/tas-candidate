package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/boltdb/bolt"
	"github.com/gorilla/mux"
)

type Candidate struct {
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Email     string `json:"Email"`
}

type TenantInfo struct {
	ShortCode string `json:"shortCode"`
}

type attributes struct {
	Entity     string   `json:"entityID"`
	Name       string   `json:"nameID"`
	Email      string   `json:"tas.personal.email"`
	FamilyName string   `json:"tas.personal.familyName"`
	GivenName  string   `json:"tas.personal.givenName"`
	Image      string   `json:"tas.personal.image"`
	Roles      []string `json:"tas.roles"`
}

var fatalLog = log.New(os.Stdout, "FATAL: ", log.LstdFlags)
var infoLog = log.New(os.Stdout, "INFO: ", log.LstdFlags)

var db *bolt.DB

var bucket = []byte("Candidates")

func register(rw http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		update(rw, req)
	} else {
		t, err := template.ParseFiles("static/candidate.html")
		infoLog.Printf("Register template error: %v", err)
		email := getEmail(req.Header.Get("tazzy-tenant"), req.Header.Get("tazzy-saml"))
		decoder := json.NewDecoder(getCandidateFromBolt(email))
		var candidate Candidate
		infoLog.Printf("Register json error: %v", decoder.Decode(&candidate))
		t.Execute(rw, candidate)
	}
}

func update(rw http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		return
	}
	email := getEmail(req.Header.Get("tazzy-tenant"), req.Header.Get("tazzy-saml"))
	candidate := Candidate{
		Email:     email,
		FirstName: req.FormValue("FirstName"),
		LastName:  req.FormValue("LastName"),
	}
	infoLog.Printf("UpdateCandidate bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)

		// Check if this is a new candidate
		data, err := json.Marshal(&candidate)
		if err == nil {
			return b.Put([]byte(candidate.Email), data)
		} else {
			return err
		}
	}))
	http.Redirect(rw, req, "/candidate/update", 301)
}

func remove(rw http.ResponseWriter, req *http.Request) {
	email := getEmail(req.Header.Get("tazzy-tenant"), req.Header.Get("tazzy-saml"))
	infoLog.Printf("Remove bolt error: %v", db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Delete([]byte(email))
	}))
	http.Redirect(rw, req, "/", 301)
}

func getCandidates(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Write(getCandidateList().Bytes())
}

func getCandidateList() *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		buffer.WriteString("[")
		k, v := c.First()
		if k != nil {
			buffer.Write(v)
			for k, v := c.Next(); k != nil; k, v = c.Next() {
				buffer.WriteString(",")
				buffer.Write(v)
			}
		}
		buffer.WriteString("]")
		return nil
	})
	return buffer
}

func getCandidateById(rw http.ResponseWriter, req *http.Request) {
	email := getEmail(req.Header.Get("tazzy-tenant"), req.Header.Get("tazzy-saml"))
	rw.Write(getCandidateFromBolt(email).Bytes())
}

func getCandidateFromBolt(email string) *bytes.Buffer {
	buffer := bytes.NewBuffer([]byte{})
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		buffer.Write(b.Get([]byte(email)))
		return nil
	})
	return buffer
}

func basePage(rw http.ResponseWriter, req *http.Request) {
	buf := getCandidateList()
	var candidates []Candidate
	decoder := json.NewDecoder(buf)
	infoLog.Printf("BasePage json error: %v", decoder.Decode(&candidates))
	t, err := template.ParseFiles("static/index.html")
	infoLog.Printf("BasePage template error: %v", err)
	if candidates == nil {
		t.Execute(rw, []Candidate{})
	} else {
		t.Execute(rw, candidates)
	}
}

func main() {
	var err error
	db, err = bolt.Open("/db/tas-candidate.db", 0644, nil)
	if err != nil {
		fatalLog.Fatal(err)
	}
	defer db.Close()

	r := mux.NewRouter()
	r.HandleFunc("/", basePage)
	r.HandleFunc("/candidate/register", register)
	r.HandleFunc("/candidate/update", register)
	r.HandleFunc("/remove/{candidate}", remove)
	r.HandleFunc("/tas/devs/tas/candidates", getCandidates)
	r.HandleFunc("/tas/devs/tas/candidates/byID/{candidate}", getCandidateById)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))
	fatalLog.Fatal(http.ListenAndServe(":8080", r))
}

func getHTTP(tenant, url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	return doHTTP(req, tenant)
}

func doHTTP(req *http.Request, tenant string) ([]byte, error) {
	req.Header.Set("tazzy-secret", os.Getenv("IO_TAZZY_SECRET"))
	req.Header.Set("tazzy-tenant", os.Getenv("APP_SHORTCODE"))
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func getEmail(tenant, saml string) string {
	url := getURL(fmt.Sprintf("core/tenants/%s/saml/assertions/byKey/%s/json", tenant, saml))
	jsonAttr, err := getHTTP(tenant, url)
	infoLog.Print(err)
	if err != nil {
		return ""
	}

	var attr attributes
	infoLog.Print(json.Unmarshal(jsonAttr, &attr))
	return attr.Email
}

func getURL(api string) string {
	return fmt.Sprintf("%s/%s", os.Getenv("IO_TAZZY_URL"), api)
}
