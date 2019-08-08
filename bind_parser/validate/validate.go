package validate

import (
	"bufio"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"math/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	ARecord recordKind = iota
	CNameRecord
	NSRecord
	MXRecord
	PTRRecord
	TXTRecord
)

type recordKind int

type HostedZone struct {
	Name            string
	ResourceRecords []RR
}

type RR struct {
	Name           string
	Kind           recordKind
	Data           string
	Priority       string
	Valid          bool
	Error          error
	TFResourceName string
	TFName         string
}

func (k recordKind) String() string {
	switch k {
	case ARecord:
		return "A"
	case CNameRecord:
		return "CNAME"
	case NSRecord:
		return "NS"
	case MXRecord:
		return "MX"
	case TXTRecord:
		return "TXT"
	case PTRRecord:
		return "PTR"
	default:
		return "unknown"
	}
}

func GetRecordKind(kindText string) recordKind {
	switch kindText {
	case "A":
		return ARecord
	case "CNAME":
		return CNameRecord
	case "MX":
		return MXRecord
	case "NS":
		return NSRecord
	case "TXT":
		return TXTRecord
	case "PTR":
		return PTRRecord
	default:
		panic("record Kind is unknown: " + kindText)
	}
}

func (r RR) IsValid() (bool, error) {
	switch r.Kind {
	case ARecord:
		return r.isValidARecord()
	case CNameRecord:
		return r.isValidCNAMERecord()
	case MXRecord:
		return r.isValidMXRecord()
	case NSRecord:
		return r.isValidNSRecord()
	case TXTRecord:
		return r.isValidTXTRecord()
	case PTRRecord:
		return r.isValidPTRRecord()
	}
	return false, nil
}

func (r RR) IsResourceRecordValid() (bool, error) {
	switch r.Kind {
	case ARecord:
		return r.isValidARecord()
	case CNameRecord:
		return r.isValidCNAMERecord()
	case MXRecord:
		return r.isValidMXRecord()
	case NSRecord:
		return r.isValidNSRecord()
	case TXTRecord:
		return r.isValidTXTRecord()
	}
	return false, nil
}

type errorString struct {
	s string
}

func (e *errorString) Error() string {
	return e.s
}

func (r RR) isValidARecord() (bool, error) {
	aRecord, err := net.LookupHost(r.Name)
	if err != nil {
		return false, errors.Wrapf(err, "A Record lookup failed for resource record: %v", r)
	}
	if !contains(aRecord, r.Data) {
		return false, &errorString{fmt.Sprintf("A records do not match: %s, public lookup: %s\n", r.Data, aRecord)}
	}
	return true, nil
}

//CNAME records do not match: premierhealthplan.gohealth.com., public lookup: premierhealthplan-independentagents.gohealth.com.
func (r RR) isValidCNAMERecord() (bool, error) {
	CNAME, err := net.LookupCNAME(r.Name)
	cnameFinal := stripDomainPeriod(CNAME)
	if err != nil {
		return false, errors.Wrapf(err, "CNAME lookup failed for resource record: %v", r)
	}
	if cnameFinal != r.Data {
		return false, &errorString{fmt.Sprintf("domain name %q, with CNAME data value: %q, does not match public lookup: %q", r.Name, r.Data, cnameFinal)}
	}
	return true, nil
}

func (r RR) isValidTXTRecord() (bool, error) {
	TXT, err := net.LookupTXT(r.Name)
	if err != nil {
		return false, errors.Wrapf(err, "TXT lookup failed for resource record: %v", r)
	}
	if !contains(TXT, strings.Trim(r.Data, "\"")) {
		return false, &errorString{fmt.Sprintf("domain name %q, TXT records do not match: %q, public lookup: %q", r.Name, strings.Trim(r.Data, "\""), TXT)}
	}
	return true, nil
}

func (r RR) isValidMXRecord() (bool, error) {
	MX, err := net.LookupMX(r.Name)
	// MX lookup return domain plus prioirty gohealth.com. 10
	// Vaildation needs to match.
	mxData := r.Data+"."
	if err != nil || len(MX) < 1 {
		return false, errors.Wrapf(err, "MX lookup failed for resource record: %v", r)
	}
	if !containsMX(MX, mxData) {
		return false, &errorString{fmt.Sprintf("MX records do not match: %s, public lookup: %s\n", mxData, MX)}
	}
	return true, nil
}

func (r RR) isValidNSRecord() (bool, error) {
	NS, err := net.LookupNS(r.Name)
	if err != nil || len(NS) < 1 {
		return false, &errorString{fmt.Sprintf("NS lookup failed for resource record: %v: error:", r, err)}
	}
	if !containsNS(NS, r.Data) {
		return false, &errorString{fmt.Sprintf("NS records do not match: %s, public DNS: %s\n", r.Data, NS)}
	}
	return true, nil
}

func (r RR) isValidPTRRecord() (bool, error) {
	return true, nil
}

func contains(lookupValue []string, localValue string) bool {
	for _, a := range lookupValue {
		if a == localValue {
			return true
		}
	}
	return false
}

func containsNS(lookupValue []*net.NS, localValue string) bool {
	for _, a := range lookupValue {
		if a.Host == localValue {
			return true
		}
	}
	return false
}

func containsMX(lookupValue []*net.MX, localValue string) bool {
	for _, a := range lookupValue {
		if a.Host == localValue {
			return true
		}
	}
	return false
}

func ParseLine(line string) []string {
	var data []string
	spaceLine := strings.Replace(line, "\t", " ", -1)
	splitLine := strings.Split(spaceLine, " ")
	var removeSpace []string
	for _, l := range splitLine {
		if l != "" {
			removeSpace = append(removeSpace, l)
		}
	}

	for _, text := range splitLine {
		if text != "" {
			data = append(data, text)
		}
	}
	return data
}

func stripDomainPeriod(domain string) string {
	var newDomain []string
	for n, char := range domain {
		if n != (len(domain) - 1) {
			newDomain = append(newDomain, string(char))
		}
	}
	return strings.Join(newDomain, "")
}

// Pass in a Bind file or a generic record set.
func GenerateResourceRecords(domainFile *os.File, ptrFiles []string) []RR {

	resourseRecords := []RR{}
	dupicateNameCheck := make(map[string]struct{})

	//log.Infof("Creating Resource Records for %s", domainFile.Name())
	scanner := bufio.NewScanner(io.Reader(domainFile))
	for scanner.Scan() {
		line := ParseLine(scanner.Text())
		recordEntry := RR{}
		if len(line) > 4 && strings.HasPrefix(line[2], "TXT") {
			domainStripSpace := strings.TrimSpace(line[0])
			var recordTextType string
			recordTextType = strings.TrimSpace(line[2])
			recordData := line[3:]
			recordEntry.Name = stripDomainPeriod(domainStripSpace)
			recordEntry.Kind = GetRecordKind(recordTextType)
			recordEntry.Data = strings.Join(recordData, " ")
			recordEntry.Valid, recordEntry.Error = recordEntry.IsValid()
			tfResourceNameReplace := strings.Replace(path.Base(recordEntry.Name), ".", "_", -1)
			tfResourceName := fmt.Sprintf("%s_%s", tfResourceNameReplace, recordEntry.Kind)

			_, exists := dupicateNameCheck[tfResourceName]
			if exists {
				unqueValue := rand.Int()
				dupicateNameCheck[fmt.Sprintf("%s_%d", tfResourceName, unqueValue)] = struct{}{}
				recordEntry.TFResourceName = fmt.Sprintf("%s_%d", tfResourceName, unqueValue)
			}else{
				dupicateNameCheck[tfResourceName] = struct{}{}
				recordEntry.TFResourceName = fmt.Sprintf("%s", tfResourceName)
			}

			recordEntry.TFName = strings.Replace(path.Base(domainFile.Name()), ".", "_", -1)
			resourseRecords = append(resourseRecords, recordEntry)
		}
		// This discards bind file entries that have the same column len as Resource Records.
		if (len(line) == 4 && !(strings.HasPrefix(line[0], "10800") || strings.HasPrefix(line[0], "54000") || strings.HasPrefix(line[0], "3600") || strings.HasPrefix(line[0], "600"))) {
			domainStripSpace := strings.TrimSpace(line[0])
			var recordTextType string
			if strings.HasPrefix(line[2], "MX") {
				recordTextType = "MX"
			} else {
				recordTextType = strings.TrimSpace(line[2])
			}
			recordData := line[3]
			recordEntry.Name = stripDomainPeriod(domainStripSpace)
			recordEntry.Kind = GetRecordKind(recordTextType)
			if recordTextType == "CNAME" || recordTextType == "MX" {
				recordEntry.Data = stripDomainPeriod(recordData)
			}else {
				recordEntry.Data = recordData
			}
			recordEntry.Valid, recordEntry.Error = recordEntry.IsValid()
			tfResourceNameReplace := strings.Replace(path.Base(recordEntry.Name), ".", "_", -1)
			tfResourceName := fmt.Sprintf("%s_%s", tfResourceNameReplace, recordEntry.Kind)

			_, exists := dupicateNameCheck[tfResourceName]
			if exists {
				unqueValue := rand.Int()
				dupicateNameCheck[fmt.Sprintf("%s_%d", tfResourceName, unqueValue)] = struct{}{}
				recordEntry.TFResourceName = fmt.Sprintf("%s_%d", tfResourceName, unqueValue)
			}else{
				dupicateNameCheck[tfResourceName] = struct{}{}
				recordEntry.TFResourceName = fmt.Sprintf("%s", tfResourceName)
			}
			recordEntry.TFName = strings.Replace(path.Base(domainFile.Name()), ".", "_", -1)
			resourseRecords = append(resourseRecords, recordEntry)
		}
		// This is necessare because TXT records can have n number of columns within the text and it was throwing off the count(excluding TXT records).
		if len(line) == 5 && !(strings.HasPrefix(line[2], "TXT")) {
			domainStripSpace := strings.TrimSpace(line[0])
			var recordTextType string
			if strings.HasPrefix(line[2], "MX") {
				recordTextType = "MX"
			} else {
				recordTextType = strings.TrimSpace(line[2])
			}
			recordData := line[4]
			recordEntry.Name = stripDomainPeriod(domainStripSpace)
			recordEntry.Kind = GetRecordKind(recordTextType)
			if recordTextType == "CNAME" || recordTextType == "MX" {
				recordEntry.Data = stripDomainPeriod(recordData)
			}else {
				recordEntry.Data = recordData
			}
			recordEntry.Priority = line[3]
			recordEntry.Valid, recordEntry.Error = recordEntry.IsValid()
			tfResourceNameReplace := strings.Replace(path.Base(recordEntry.Name), ".", "_", -1)
			tfResourceName := fmt.Sprintf("%s_%s", tfResourceNameReplace, recordEntry.Kind)

			_, exists := dupicateNameCheck[tfResourceName]
			if exists {
				unqueValue := rand.Int()
				dupicateNameCheck[fmt.Sprintf("%s_%d", tfResourceName, unqueValue)] = struct{}{}
				recordEntry.TFResourceName = fmt.Sprintf("%s_%d", tfResourceName, unqueValue)
			}else{
				dupicateNameCheck[tfResourceName] = struct{}{}
				recordEntry.TFResourceName = fmt.Sprintf("%s", tfResourceName)
			}
			recordEntry.TFName = strings.Replace(path.Base(domainFile.Name()), ".", "_", -1)
			resourseRecords = append(resourseRecords, recordEntry)
		}
	}
	log.Infof("Creating PTR Records for %s", domainFile.Name())
	for _, x := range ptrFiles {
		ptrFile, err := os.Open(x+".arpa")
		if err != nil{
			errors.Wrapf(err, "failed to open file: %s", ptrFile)
		}
		ptrScanner := bufio.NewScanner(io.Reader(ptrFile))
		for ptrScanner.Scan() {
			recordEntry := RR{}
			line := ParseLine(ptrScanner.Text())
			if len(line) == 4 && !(strings.HasPrefix(line[0], "10800") || strings.HasPrefix(line[0], "54000") || strings.HasPrefix(line[0], "3600") || strings.HasPrefix(line[0], "600")){
				//log.Infof("PTR bit: %+v, type: %+v, domain: %+v", line[0], line[2], line[3]
				ptrDomain := stripDomainPeriod(line[3])
				splitPTRDomain := strings.SplitN(ptrDomain, ".", 2)
				if ptrDomain == filepath.Base(domainFile.Name()){
					log.Infof("Domain MATCH! %s == %s, ptrRecord %s", ptrDomain, filepath.Base(domainFile.Name()),filepath.Base(x)+"."+line[0] )
					var recordTextType string
					recordTextType = line[2]
					recordData := filepath.Base(x)+"."+line[0]
					recordEntry.Name = stripDomainPeriod(line[3])
					recordEntry.Kind = GetRecordKind(recordTextType)
					recordEntry.Data = recordData
					recordEntry.Valid, recordEntry.Error = recordEntry.IsValid()
					tfResourceNameReplace := strings.Replace(path.Base(recordEntry.Name), ".", "_", -1)
					tfResourceName := fmt.Sprintf("%s_%s", tfResourceNameReplace, recordEntry.Kind)
					recordEntry.TFResourceName = fmt.Sprintf("%s", tfResourceName)
					recordEntry.TFName = strings.Replace(path.Base(domainFile.Name()), ".", "_", -1)
					resourseRecords = append(resourseRecords, recordEntry)
				}else if splitPTRDomain[1] == filepath.Base(domainFile.Name()) {
					log.Infof("PTR Domain %s - SubDomain MATCH! %s == %s, ptrRecord: %s", ptrDomain, splitPTRDomain[1], filepath.Base(domainFile.Name()), filepath.Base(x)+"."+line[0] )
					var recordTextType string
					recordTextType = line[2]
					recordData := filepath.Base(x)+"."+line[0]
					recordEntry.Name = stripDomainPeriod(line[3])
					recordEntry.Kind = GetRecordKind(recordTextType)
					recordEntry.Data = recordData
					recordEntry.Valid, recordEntry.Error = recordEntry.IsValid()
					tfResourceNameReplace := strings.Replace(path.Base(recordEntry.Name), ".", "_", -1)
					tfResourceName := fmt.Sprintf("%s_%s", tfResourceNameReplace, recordEntry.Kind)
					recordEntry.TFResourceName = fmt.Sprintf("%s", tfResourceName)
					recordEntry.TFName = strings.Replace(path.Base(domainFile.Name()), ".", "_", -1)
					resourseRecords = append(resourseRecords, recordEntry)
				}
			}
			//recordEntry := RR{}
		}
	}
	log.Debugf("Duplicate type check: %+v", dupicateNameCheck)
	return resourseRecords
}
