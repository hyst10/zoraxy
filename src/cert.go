package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"imuslab.com/zoraxy/mod/utils"
)

// Check if the default certificates is correctly setup
func handleDefaultCertCheck(w http.ResponseWriter, r *http.Request) {
	type CheckResult struct {
		DefaultPubExists bool
		DefaultPriExists bool
	}

	pub, pri := tlsCertManager.DefaultCertExistsSep()
	js, _ := json.Marshal(CheckResult{
		pub,
		pri,
	})

	utils.SendJSONResponse(w, string(js))
}

// Return a list of domains where the certificates covers
func handleListCertificate(w http.ResponseWriter, r *http.Request) {
	filenames, err := tlsCertManager.ListCertDomains()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	showDate, _ := utils.GetPara(r, "date")
	if showDate == "true" {
		type CertInfo struct {
			Domain           string
			LastModifiedDate string
			ExpireDate       string
		}

		results := []*CertInfo{}

		for _, filename := range filenames {
			certFilepath := filepath.Join(tlsCertManager.CertStore, filename+".crt")
			//keyFilepath := filepath.Join(tlsCertManager.CertStore, filename+".key")
			fileInfo, err := os.Stat(certFilepath)
			if err != nil {
				utils.SendErrorResponse(w, "invalid domain certificate discovered: "+filename)
				return
			}
			modifiedTime := fileInfo.ModTime().Format("2006-01-02 15:04:05")

			certExpireTime := "Unknown"
			certBtyes, err := os.ReadFile(certFilepath)
			if err != nil {
				//Unable to load this file
				continue
			} else {
				//Cert loaded. Check its expire time
				block, _ := pem.Decode(certBtyes)
				if block != nil {
					cert, err := x509.ParseCertificate(block.Bytes)
					if err == nil {
						certExpireTime = cert.NotAfter.Format("2006-01-02 15:04:05")
					}
				}
			}

			thisCertInfo := CertInfo{
				Domain:           filename,
				LastModifiedDate: modifiedTime,
				ExpireDate:       certExpireTime,
			}

			results = append(results, &thisCertInfo)
		}

		js, _ := json.Marshal(results)
		w.Header().Set("Content-Type", "application/json")
		w.Write(js)
	} else {
		response, err := json.Marshal(filenames)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	}

}

// Handle front-end toggling TLS mode
func handleToggleTLSProxy(w http.ResponseWriter, r *http.Request) {
	currentTlsSetting := false
	if sysdb.KeyExists("settings", "usetls") {
		sysdb.Read("settings", "usetls", &currentTlsSetting)
	}

	newState, err := utils.PostPara(r, "set")
	if err != nil {
		//No setting. Get the current status
		js, _ := json.Marshal(currentTlsSetting)
		utils.SendJSONResponse(w, string(js))
	} else {
		if newState == "true" {
			sysdb.Write("settings", "usetls", true)
			log.Println("Enabling TLS mode on reverse proxy")
			dynamicProxyRouter.UpdateTLSSetting(true)
		} else if newState == "false" {
			sysdb.Write("settings", "usetls", false)
			log.Println("Disabling TLS mode on reverse proxy")
			dynamicProxyRouter.UpdateTLSSetting(false)
		} else {
			utils.SendErrorResponse(w, "invalid state given. Only support true or false")
			return
		}

		utils.SendOK(w)

	}
}

// Handle the GET and SET of reverse proxy TLS versions
func handleSetTlsRequireLatest(w http.ResponseWriter, r *http.Request) {
	newState, err := utils.PostPara(r, "set")
	if err != nil {
		//GET
		var reqLatestTLS bool = false
		if sysdb.KeyExists("settings", "forceLatestTLS") {
			sysdb.Read("settings", "forceLatestTLS", &reqLatestTLS)
		}

		js, _ := json.Marshal(reqLatestTLS)
		utils.SendJSONResponse(w, string(js))
	} else {
		if newState == "true" {
			sysdb.Write("settings", "forceLatestTLS", true)
			log.Println("Updating minimum TLS version to v1.2 or above")
			dynamicProxyRouter.UpdateTLSVersion(true)
		} else if newState == "false" {
			sysdb.Write("settings", "forceLatestTLS", false)
			log.Println("Updating minimum TLS version to v1.0 or above")
			dynamicProxyRouter.UpdateTLSVersion(false)
		} else {
			utils.SendErrorResponse(w, "invalid state given")
		}
	}
}

// Handle upload of the certificate
func handleCertUpload(w http.ResponseWriter, r *http.Request) {
	// check if request method is POST
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// get the key type
	keytype, err := utils.GetPara(r, "ktype")
	overWriteFilename := ""
	if err != nil {
		http.Error(w, "Not defined key type (pub / pri)", http.StatusBadRequest)
		return
	}

	// get the domain
	domain, err := utils.GetPara(r, "domain")
	if err != nil {
		//Assume localhost
		domain = "default"
	}

	if keytype == "pub" {
		overWriteFilename = domain + ".crt"
	} else if keytype == "pri" {
		overWriteFilename = domain + ".key"
	} else {
		http.Error(w, "Not supported keytype: "+keytype, http.StatusBadRequest)
		return
	}

	// parse multipart form data
	err = r.ParseMultipartForm(10 << 20) // 10 MB
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	// get file from form data
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// create file in upload directory
	os.MkdirAll("./certs", 0775)
	f, err := os.Create(filepath.Join("./certs", overWriteFilename))
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// copy file contents to destination file
	_, err = io.Copy(f, file)
	if err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// send response
	fmt.Fprintln(w, "File upload successful!")
}

// Handle cert remove
func handleCertRemove(w http.ResponseWriter, r *http.Request) {
	domain, err := utils.PostPara(r, "domain")
	if err != nil {
		utils.SendErrorResponse(w, "invalid domain given")
		return
	}
	err = tlsCertManager.RemoveCert(domain)
	if err != nil {
		utils.SendErrorResponse(w, err.Error())
	}
}
