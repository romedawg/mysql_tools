package terraform

import (
	"bytes"
	"fmt"
	"github.com/onrik/logrus/filename"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"strings"
	"text/template"

	"bb.dev.norvax.net/dep/operator/aws/plesk_route53/template"
	"bb.dev.norvax.net/dep/operator/aws/plesk_route53/validate"
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

// Output for terraform main module domain_com.tf
func generateHostedZoneFile(hostedZone *validate.HostedZone, file *os.File) {

	file.WriteString(fmt.Sprintf(`resource "aws_route53_zone" %q {`, strings.Replace(hostedZone.Name, ".", "_", -1)))
	file.WriteString("\n")
	file.WriteString(fmt.Sprintf(`  name    = "%s"`, hostedZone.Name))
	file.WriteString("\n")
	file.WriteString(fmt.Sprintf(`  delegation_set_id    = "${var.delegation_set_id}"`))
	file.WriteString("\n")
	file.WriteString("}")
	file.Close()
}

// Returns Hosted Zones from any bind file(or resource records passed in)
func GenerateHostedZones(fileName string, ptrFiles []string)*validate.HostedZone{

	hostedZone := &validate.HostedZone{}
	pleskBindfile, err := os.Open(fileName)
	if err != nil {
		errors.Wrapf(err, "could not open: %s", pleskBindfile)
	}

	resourseRecords := validate.GenerateResourceRecords(pleskBindfile, ptrFiles)
	hostedZone.Name = path.Base(pleskBindfile.Name())

	hostedZone.ResourceRecords = resourseRecords

	pleskBindfile.Close()
	return hostedZone
}

func createTerraforResoureFileAppends(filename string)*os.File{

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		file, err := os.Create(filename)
		if err != nil {
			errors.Wrapf(err, "failed to create filename: %s", filename)
		}

		openedFile, err := os.OpenFile(file.Name(), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		return openedFile
	}else{
		openedFile, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		return openedFile
	}
}

// This will create a locals and variables file in the top level directory.
func GenerateLocalVariableFiles(hostedZoneDir string){

	log.Debugf("Creating Locals/Variables files: %s", hostedZoneDir)

	localsFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "locals.tf"))
	localsTmpl := template.New(templating.LocalsFile)
	localsTmpl.DefinedTemplates()
	localsFile.WriteString(localsTmpl.Name())

	outputFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "output.tf"))
	outputTmpl := template.New(templating.OutputFile)
	outputTmpl.DefinedTemplates()
	outputFile.WriteString(outputTmpl.Name())

}
func DelegationSetToMainModuleTerraform(filename string) {

	log.Debugf("Terraform main: adding delegation set to %s", filename)
	terraformMainModule, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		errors.Wrapf(err, "could not open file %s", filename)
	}

	terraformMainModule.WriteString(`resource "aws_route53_delegation_set" "gohealth" {`)
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString(`  reference_name = "GoHealthNS"`)
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString("}")
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString("\n")
	terraformMainModule.Close()
}


// this is the terraform main file that will append each domain that terraform will need to load in.
func AppendZoneToMainModuleTerraform(hostedZone *validate.HostedZone, filename string){

	log.Debugf("Terraform main: appending %s to %s", hostedZone.Name, filename)
	terraformMainModule, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		errors.Wrapf(err, "could not open file %s", filename)
	}

	terraformMainModule.WriteString(fmt.Sprintf(`module "%s_hosted_zone" {`, strings.Replace(hostedZone.Name, ".", "_", -1)))
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString(fmt.Sprintf(`  source = "./%s"`, hostedZone.Name))
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString(fmt.Sprintf(`  ttl    = "${local.ttl}"`))
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString(fmt.Sprintf(`  delegation_set_id    = "${aws_route53_delegation_set.gohealth.id}"`))
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString("}")
	terraformMainModule.WriteString("\n")
	terraformMainModule.WriteString("\n")

	terraformMainModule.Close()
}

func generateTerraformResourceRecord(hostedZone *validate.HostedZone, hostedZoneDir string){

	// Create variables.tf file in each of the Domain Directory
	variablesFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "variables.tf"))
	tmpl := template.New(templating.VariablesFile)
	tmpl.DefinedTemplates()
	variablesFile.WriteString(tmpl.Name())

	for _, record := range hostedZone.ResourceRecords {
		// Need to ensure the value exists
		if record.Kind == validate.GetRecordKind("A") {
			ARecordFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "a_record.tf"))
			defer ARecordFile.Close()

			tmpl := template.Must(template.New("recordtpl").Parse(templating.RecordtmplDefinition))
			aString := bytes.NewBufferString("")
			tmpl.Execute(aString, record)
			ARecordFile.WriteString(aString.String())
		} else if record.Kind == validate.GetRecordKind("CNAME") {
			CNAMERecordFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "cname_record.tf"))
			defer CNAMERecordFile.Close()
			tmpl := template.Must(template.New("cnamerecordtpl").Parse(templating.RecordtmplDefinition))
			cnameString := bytes.NewBufferString("")
			tmpl.Execute(cnameString, record)
			CNAMERecordFile.WriteString(cnameString.String())
		} else if record.Kind == validate.GetRecordKind("MX") {
			MXRecordFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "mx_record.tf"))
			defer MXRecordFile.Close()
			tmpl := template.Must(template.New("mxrecordtpl").Parse(templating.MXRecordtmplDefinition))
			mxString := bytes.NewBufferString("")
			tmpl.Execute(mxString, record)
			MXRecordFile.WriteString(mxString.String())
		} else if record.Kind == validate.GetRecordKind("TXT") {
			TXTRecordFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "txt_record.tf"))
			defer TXTRecordFile.Close()
			tmpl := template.Must(template.New("TXTrecordtpl").Parse(templating.TXTRecordtmplDefinition))
			txtString := bytes.NewBufferString("")
			tmpl.Execute(txtString, record)
			TXTRecordFile.WriteString(txtString.String())
		} else if record.Kind == validate.GetRecordKind("PTR") {
			PTRRecordFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "ptr_record.tf"))
			defer PTRRecordFile.Close()
			tmpl := template.Must(template.New("PTRrecordtpl").Parse(templating.PTRRecordtmplDefinition))
			ptrString := bytes.NewBufferString("")
			tmpl.Execute(ptrString, record)
			PTRRecordFile.WriteString(ptrString.String())
		}
		//}else if record.Kind == validate.GetRecordKind("NS") {
		//NSRecordFile := createTerraforResoureFileAppends(path.Join(hostedZoneDir, "ns_record.tf"))
		//defer NSRecordFile.Close()
		//tmpl := template.Must(template.New("nsrecordtpl").Parse(templating.NSRecordtmplDefinition))
		//nsString := bytes.NewBufferString("")
		//tmpl.Execute(nsString, record)
		//NSRecordFile.WriteString(nsString.String())
		//}
	}
}

func CreateTerraformResources(hostedZone *validate.HostedZone, terraformDir string, mainTerraformFileName string)error {


	hostedZoneDir := path.Join(terraformDir, hostedZone.Name)
	// Create Directory: hostedZone.name
	err := os.Mkdir(hostedZoneDir, 0777)
	if err != nil {
		errors.Wrapf(err, "failed to create the outputdir")
	}

	// Create Hosted zone terraform file - domainName_tf(i.e gohealth_com.tf)
	terraformDomainMainFile := fmt.Sprintf("%s.tf",strings.Replace(hostedZone.Name, ".","_", -1))
	log.Debugf("creating file %s\n",  path.Join(hostedZoneDir, terraformDomainMainFile))
	terraformMainFile, err := os.Create(path.Join(hostedZoneDir, terraformDomainMainFile))
	if err != nil {
		errors.Wrapf(err, "could not create file: %s", terraformMainFile)
	}

	terraformFile, err := os.OpenFile(terraformMainFile.Name(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}

	AppendZoneToMainModuleTerraform(hostedZone, mainTerraformFileName)
	if err != nil {
		errors.Wrapf(err, "failed to create terraform main file.: %s", path.Join(terraformDir, "main.tf"))
	}

	defer terraformFile.Close()
	log.Debugf("generating hosted zone terraform %s\n", terraformDomainMainFile)
	generateHostedZoneFile(hostedZone, terraformFile)
	generateTerraformResourceRecord(hostedZone, hostedZoneDir)
	return nil
}