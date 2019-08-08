package templating

import (
	"bytes"
	"fmt"
	"text/template"
)

type RR struct {
	Name   string
	TFName string
	Kind   string
	Data   string
	ZoneId string
	TTL    int
}

var (
	RecordtmplDefinition = `resource "aws_route53_record" "{{.TFResourceName}}" {
  name    = "{{.Name}}"
  ttl     = "${var.ttl}"
  type    = "{{.Kind}}"
  zone_id = "${aws_route53_zone.{{.TFName}}.zone_id}"

  records = [
    "{{.Data}}",
  ]
}

`
	TXTRecordtmplDefinition = `resource "aws_route53_record" "{{.TFResourceName}}" {
  name    = "{{.Name}}"
  ttl     = "${var.ttl}"
  type    = "{{.Kind}}"
  zone_id = "${aws_route53_zone.{{.TFName}}.zone_id}"

  records = [
    {{.Data}},
  ]
}

`
    NSRecordtmplDefinition = `resource "aws_route53_record" "{{.TFResourceName}}" {
  allow_overwrite = true
  name    = "{{.Name}}"
  ttl     = "${var.ttl}"
  type    = "{{.Kind}}"
  zone_id = "${aws_route53_zone.{{.TFName}}.zone_id}"

  records = [
    "ns-98.awsdns-12.com",
    "ns-850.awsdns-42.net",
    "ns-1366.awsdns-42.org",
    "ns-1759.awsdns-27.co.uk"
  ]
}

`

	MXRecordtmplDefinition = `resource "aws_route53_record" "{{.TFResourceName}}" {
  name    = "{{.Name}}"
  ttl     = "${var.ttl}"
  type    = "{{.Kind}}"
  zone_id = "${aws_route53_zone.{{.TFName}}.zone_id}"

  records = [
    "{{.Priority}} {{.Data}}",
  ]
}
`


	PTRRecordtmplDefinition = `resource "aws_route53_record" "{{.TFResourceName}}" {
  name    = "{{.Name}}"
  ttl     = "${var.ttl}"
  type    = "{{.Kind}}"
  zone_id = "${aws_route53_zone.{{.TFName}}.zone_id}"

  records = [
    "{{.Data}}",
  ]
}
`

    VariablesFile = `variable ttl {}
					 variable delegation_set_id {}
`

    LocalsFile = `locals {
  ttl = 300
}`

    OutputFile = `output "ttl" {
  value = "${local.ttl}"
}
output "delegations_set_id" {
  value = "${aws_route53_delegation_set.gohealth.id}"

}`

	MainModuleFile = `module "{{.Domain}}_hosted_zone" {
						source = {{.Domain}}
						ttl    = "${local.ttl}"
}`
)

//terraformMainModule.WriteString(fmt.Sprintf(`module "%s_hosted_zone" {`, strings.Replace(hostedZone.Name, ".", "_", -1)))
//terraformMainModule.WriteString("\n")
//terraformMainModule.WriteString(fmt.Sprintf(`  source = "./%s"`, hostedZone.Name))
//terraformMainModule.WriteString("\n")
//terraformMainModule.WriteString(fmt.Sprintf(`  ttl    = "${local.ttl}"`))
//terraformMainModule.WriteString("\n")
//terraformMainModule.WriteString("}")
//terraformMainModule.WriteString("\n")
//terraformMainModule.WriteString("\n")

// not used anywhere
func test() {
	arecord := RR{
		Name:   "aetnaplans.com",
		TFName: "aetnaplans_com",
		Kind:   "A",
		Data:   "69.20.102.128",
		TTL:    85000,
		ZoneId: `${aws_route53_zone.apply_norvax_com.zone_id}`,
	}

	tmpl := template.Must(template.New("arecordtpl").Parse(RecordtmplDefinition))

	sb := bytes.NewBufferString("")
	tmpl.Execute(sb, arecord)

	fmt.Println(sb.String())
}
