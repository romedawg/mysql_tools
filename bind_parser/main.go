package main

import (
	"bb.dev.norvax.net/dep/operator/aws/plesk_route53/terraform"
	"bb.dev.norvax.net/dep/operator/aws/plesk_route53/validate"
	"bufio"
	"flag"
	"fmt"
	"github.com/onrik/logrus/filename"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)


func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	log.SetLevel(log.DebugLevel)
	filenameHook := filename.NewHook()
	filenameHook.Field = "source"
	log.AddHook(filenameHook)
}


var (
	dir       = flag.String("dir", "", "directory to search through bind files")
	outputDir = flag.String("outputDir", "", "directory ouptut yml and type record files")
	debug       = flag.Bool("debug", false, "change log level to debug(default: false)")
)

func setup() error {
	flag.Parse()
	if *dir == "" {
		return errors.New("failed to pass in a directory")
	}
	if *outputDir == "" {
		return errors.New("failed to pass in a directory")
	}

	err := os.Mkdir(*outputDir, 0777)
	if err != nil {
		errors.Wrapf(err, "failed to create the outputdir")
	}

	if *debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	return nil
}

func returnBindFileNames(dir string) ([]string, []string) {
	domains := make([]string, 0)
	ptrRecords := make([]string, 0)

	dd, err := ioutil.ReadDir(dir)
	if err != nil {
		errors.Wrapf(err, "could not open: %s", dir)
	}

	for _, x := range dd {
		if strings.HasSuffix(x.Name(), "arpa"){
			stripArpa := strings.TrimSuffix(x.Name(), ".arpa")
			ptrRecords = append(ptrRecords,path.Join(dir, stripArpa))
		} else {
			domains = append(domains, path.Join(dir, x.Name()))
		}
	}
	return domains, ptrRecords
}

// read plesk bindfile and output the Resource Records as a CSV to the outputDir that is passed in.
func processPleskBindFiles(domainFile string, ptrFiles []string, pleskDir string, outputDir string) []validate.RR {
	domainBaseName := path.Base(domainFile)

	// The bindFile will be the full path
	pleskBindfile, err := os.Open(domainFile)
	if err != nil {
		errors.Wrapf(err, "could no open: %s", pleskBindfile)
	}

	// Creates a copy of the plesk bind files with just the resource records(A,CNAME, etc..)
	createDomainFile, err := os.Create(path.Join(pleskDir, domainBaseName))
	if err != nil {
		errors.Wrapf(err, "failed to create Domain Resource file: %s", path.Join(pleskDir, domainBaseName))
	}

	createDomainFileWithErrors, err := os.OpenFile(path.Join(outputDir, "plesk_domains_with_dns_errors.csv"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}

	defer pleskBindfile.Close()
	defer createDomainFile.Close()
	defer createDomainFileWithErrors.Close()
	// Now you can iterate through pleskBindfile and output results to createDomainFile
	resourseRecords := validate.GenerateResourceRecords(pleskBindfile, ptrFiles)

	// This will create a copy of the plesk domain file(again, just the resource records)
	log.Debugf("validating and created file for %v", domainBaseName)
	for _, record := range resourseRecords{
		if !(record.Kind == validate.GetRecordKind("MX")) {
			createDomainFile.Write([]byte(fmt.Sprintf("%s, %s, %s, %v\n", record.Name, record.Kind, record.Data, record.Valid)))
			createDomainFileWithErrors.Write([]byte(fmt.Sprintf("%s, %s, %s, %s, %v, %v\n", domainBaseName, record.Name, record.Kind, record.Data, record.Valid, strconv.Quote(fmt.Sprintf("%v",record.Error)))))
		}else {
			createDomainFile.Write([]byte(fmt.Sprintf("%s, %s, %s, %s, %v\n", record.Name, record.Kind, record.Priority, record.Data, record.Valid)))
			createDomainFileWithErrors.Write([]byte(fmt.Sprintf("%s, %s, %s, %s, %v, %v\n", domainBaseName, record.Name, record.Kind, record.Data+". "+ record.Priority, record.Valid, strconv.Quote(fmt.Sprintf("%v",record.Error)))))
		}
	}
	return resourseRecords
}

// take ptr files(full path) and create a csv file in /outputDir/plesk
func processPTRFiles(ptrFiles []string, outDir string)[]string{

	var ptrFilesSlice []string
	//ptrFiles is the full path name
	for _, x := range ptrFiles {
		log.Debugf("file is: %+v", x)
		ptrBindfile, err := os.Open(x+".arpa")
		if err != nil {
			errors.Wrapf(err, "could no open: %s", ptrBindfile)
		}

		createPTRFileCopy, err := os.Create(path.Join(outDir, path.Base(x)))
		if err != nil {
			errors.Wrapf(err, "failed to create Domain Resource file: %s", path.Join(outDir, createPTRFileCopy.Name()))
		}
		createPTRFileCopy.Close()
		ptrFile, err := os.OpenFile(createPTRFileCopy.Name(), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		log.Debugf("reading from file: %+v", ptrBindfile.Name())
		log.Debugf("attempting to wrtie to file: %s", createPTRFileCopy.Name())

		scanner := bufio.NewScanner(io.Reader(ptrBindfile))
		log.Debugf("about to for Scan")
		for scanner.Scan() {
			line := validate.ParseLine(scanner.Text())
			if len(line) == 4 && !(strings.HasPrefix(line[0], "10800") || strings.HasPrefix(line[0], "54000") || strings.HasPrefix(line[0], "3600") || strings.HasPrefix(line[0], "600")){
				ptrFile.Write([]byte(fmt.Sprintf("%s, %s, %s, %s\n", line[0], line[1], line[2], line[3])))
				createPTRFileCopy.Close()
			}
		}
		ptrFile.Close()
		// this could probably be a string createPTRFileCopy.Name()
		// would have to change downstream functions
		ptrFilesSlice = append(ptrFilesSlice, x)
	}

	return ptrFilesSlice

}

func main() {

	// 4 objectives
	// 1. Record Resources Domain File: parse plesk bind files and output to a standard file(domainFile as file Name and a list of record resources)
	// 2. Terraform Hosted Zones: parse Record resources and output a
	// 3. Hosted Zone Cloud stacks: A hosted zones Resource Records(A,CNAME, MX, TXT)
	// 4. Using the Recourse Domain File: Validate Entries- ? this is questionable in terms of results.

	err := setup()
	if err != nil {
		flag.Usage()
		log.Fatalln(err)
	}

	// /outputDir/plesk <- where backups of the plesk domains are saved.
	pleskDir := path.Join(*outputDir, "plesk")
	err = os.Mkdir(pleskDir, 0777)
	if err != nil {
		errors.Wrapf(err, "failed to create the outputdir")
	}

	// slices of strings(fullpath name of the file)
	bindFileNames, ptrFileNames := returnBindFileNames(*dir)

	log.Infof("creating PTR bind Files from plesk")
	ptrFiles := processPTRFiles(ptrFileNames, pleskDir)

	_, err = os.Create(path.Join(*outputDir, "plesk_domains_with_dns_errors.csv"))
	if err != nil {
		errors.Wrap(err, "failed to create Domain Resource file")
	}

	terraformDir := path.Join(*outputDir, "terraform")
	log.Debugf("creating dir %s\n", terraformDir)
	err = os.Mkdir(terraformDir, 0777)
	if err != nil {
		errors.Wrapf(err, "failed to create the outputdir")
	}

	// Create main.tf to import all of the domains into terraform
	mainTerraformFileName := path.Join(terraformDir, "main.tf")
	_, err = os.Stat(mainTerraformFileName); if os.IsNotExist(err){
		_, err := os.Create(mainTerraformFileName)
		if err != nil {
			errors.Wrapf(err, "could not create file: %s", path.Join(terraformDir, "main.tf"))
		}
	}

	terraform.DelegationSetToMainModuleTerraform(mainTerraformFileName)

	for _, domainFile := range bindFileNames{
		log.Debugf("processing bind filename: %s\n", domainFile)
		_ = processPleskBindFiles(domainFile, ptrFiles, pleskDir, *outputDir)

		// Generate Terraform Resource Files(main.tf and resource Records).
		log.Infof("creating Hostedzone and terraform resource for %s", filepath.Base(domainFile))
		hostedZoneRecord := terraform.GenerateHostedZones(domainFile, ptrFiles)
		terraform.CreateTerraformResources(hostedZoneRecord, terraformDir, mainTerraformFileName)
	}
	terraform.GenerateLocalVariableFiles(path.Join(*outputDir, "terraform"))
}
