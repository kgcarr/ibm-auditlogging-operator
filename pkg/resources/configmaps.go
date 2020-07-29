//
// Copyright 2020 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package resources

import (
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	operatorv1 "github.com/ibm/ibm-auditlogging-operator/pkg/apis/operator/v1"
	operatorv1alpha1 "github.com/ibm/ibm-auditlogging-operator/pkg/apis/operator/v1alpha1"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnableAuditLogForwardKey defines the key in the source config map for turning audit on or off
const EnableAuditLogForwardKey = "ENABLE_AUDIT_LOGGING_FORWARDING"

// HTTPIngestURLKey defines the Http endpoint
const HTTPIngestURLKey = "AuditLoggingHttpIngestURL"

// ConfigName defines the name of the config configmap
const ConfigName = "config"

// FluentdConfigName defines the name of the volume for the config configmap
const FluentdConfigName = "main-config"

// SourceConfigName defines the name of the source-config configmap
const SourceConfigName = "source-config"

// QRadarConfigName defines the name of the remote-syslog-config configmap
const QRadarConfigName = "remote-syslog-config"

// SplunkConfigName defines the name of the splunk-hec-config configmap
const SplunkConfigName = "splunk-hec-config"

// HTTPIngestName defines the name of the http-ingesturl configmap
const HTTPIngestName = "http-ingesturl"

// FluentdConfigKey defines the key for the config configmap
const FluentdConfigKey = "fluent.conf"

// SourceConfigKey defines the key for the source-config configmap
const SourceConfigKey = "source.conf"

// SplunkConfigKey defines the key for the splunk-hec-config configmap
const SplunkConfigKey = "splunkHEC.conf"

//QRadarConfigKey defines the key for the remote-syslog-config configmap
const QRadarConfigKey = "remoteSyslog.conf"

// OutputPluginMatches defines the match tags for Splunk and QRadar outputs
const OutputPluginMatches = "icp-audit icp-audit.**"

const httpPath = "/icp-audit.http"

var fluentdMainConfigData = `
fluent.conf: |-
  # Input plugins (Supports Systemd and HTTP)
  @include /fluentd/etc/source.conf

  # Output plugins (Only use one output plugin conf file at a time. Comment or remove other files)
  #@include /fluentd/etc/remoteSyslog.conf
  #@include /fluentd/etc/splunkHEC.conf
`
var sourceConfigDataKey = `
source.conf: |-`

var sourceConfigDataSystemd1 = `
    <source>
        @type systemd
        @id input_systemd_icp
        @log_level info
        tag icp-audit
        path `
var sourceConfigDataSystemd2 = `
        matches '[{ "SYSLOG_IDENTIFIER": "icp-audit" }]'
        read_from_head true
        <storage>
          @type local
          persistent true
          path /icp-audit
        </storage>
        <entry>
          fields_strip_underscores true
          fields_lowercase true
        </entry>
    </source>`
var sourceConfigDataHTTP1 = `
    <source>
        @type http
        # Tag is not supported in yaml, must be set by request path (/icp-audit.http is required for validation and export)
        port `
var sourceConfigDataHTTP2 = `
        bind 0.0.0.0
        body_size_limit 32m
        keepalive_timeout 10s
        <transport tls>
          ca_path /fluentd/etc/https/ca.crt
          cert_path /fluentd/etc/https/tls.crt
          private_key_path /fluentd/etc/https/tls.key
        </transport>
        <parse>
          @type json
        </parse>
    </source>
`
var filterJournal = `
    <filter icp-audit>
        @type parser
        format json
        key_name message
        reserve_data true
    </filter>
`
var filterHTTP = `
    <filter icp-audit.*>
        @type record_transformer
        enable_ruby true
        <record>
          tag ${tag}
          message ${record.to_json}
        </record>
    </filter>`

var splunkConfigData1 = `
splunkHEC.conf: |-
    <match icp-audit icp-audit.**>`
var splunkDefaults = `
        @type splunk_hec
        hec_host SPLUNK_SERVER_HOSTNAME
        hec_port SPLUNK_PORT
        hec_token SPLUNK_HEC_TOKEN
        ca_file /fluentd/etc/tls/splunkCA.pem
        source ${tag}`
var splunkConfigData2 = `
    </match>`

var qRadarConfigData1 = `
remoteSyslog.conf: |-
    <match icp-audit icp-audit.**>`
var qRadarDefaults = `
        @type copy
        <store>
            @type remote_syslog
            host QRADAR_SERVER_HOSTNAME
            port QRADAR_PORT_FOR_icp-audit
            hostname QRADAR_LOG_SOURCE_IDENTIFIER_FOR_icp-audit
            protocol tcp
            tls true
            ca_file /fluentd/etc/tls/qradar.crt
            packet_size 4096
            program fluentd
            <format>
                @type single_value
                message_key message
            </format>
        </store>`
var qRadarConfigData2 = `
    </match>`

// operatorv1 output config constants
var fluentdMainConfigV1Data = `
fluent.conf: |-
    # Input plugins (Supports Systemd and HTTP)
    @include /fluentd/etc/source.conf
    # Output plugins (Supports Splunk and Syslog)`
var fluentdOutputConfigV1Data = `
    <match icp-audit icp-audit.**>
        @type copy
`
var splunkConfigV1Data1 = `
splunkHEC.conf: |-
    <store>
        @type splunk_hec
`
var splunkConfigV1Data2 = `
        ca_file /fluentd/etc/tls/splunkCA.pem
        source ${tag}
    </store>`
var qRadarConfigV1Data1 = `
remoteSyslog.conf: |-
    <store>
        @type remote_syslog
`
var qRadarConfigV1Data2 = `
        protocol tcp
        ca_file /fluentd/etc/tls/qradar.crt
        packet_size 4096
        program fluentd
        <format>
            @type single_value
            message_key message
        </format>
    </store>`

// FluentdConfigMaps defines the names of the fluentd configmaps
var FluentdConfigMaps = []string{
	FluentdDaemonSetName + "-" + ConfigName,
	FluentdDaemonSetName + "-" + SourceConfigName,
	FluentdDaemonSetName + "-" + SplunkConfigName,
	FluentdDaemonSetName + "-" + QRadarConfigName,
	FluentdDaemonSetName + "-" + HTTPIngestName,
}

// DataSplunk defines the struct for splunk-hec-config
type DataSplunk struct {
	Value string `yaml:"splunkHEC.conf"`
}

// DataQRadar defines the struct for remote-syslog-config
type DataQRadar struct {
	Value string `yaml:"remoteSyslog.conf"`
}

const hecHost = `hec_host `
const hecPort = `hec_port `
const hecToken = `hec_token `
const host = `host `
const port = `port `
const hostname = `hostname `
const protocol = `protocol `
const tls = `tls `

var RegexHecHost = regexp.MustCompile(hecHost + `.*`)
var RegexHecPort = regexp.MustCompile(hecPort + `.*`)
var RegexHecToken = regexp.MustCompile(hecToken + `.*`)
var RegexHost = regexp.MustCompile(host + `.*`)
var RegexPort = regexp.MustCompile(port + `.*`)
var RegexHostname = regexp.MustCompile(hostname + `.*`)
var RegexProtocol = regexp.MustCompile(protocol + `.*`)
var RegexTLS = regexp.MustCompile(tls + `.*`)

var qradarPlugin = `@include /fluentd/etc/remoteSyslog.conf`
var splunkPlugin = `@include /fluentd/etc/splunkHEC.conf`

const matchTags = `<match icp-audit icp-audit.**>`

var Protocols = map[bool]string{
	true:  "https",
	false: "http",
}

// BuildFluentdConfigMap returns a ConfigMap object
func BuildFluentdConfigMap(instance *operatorv1.CommonAudit, name string) (*corev1.ConfigMap, error) {
	reqLogger := log.WithValues("ConfigMap.Namespace", instance.Namespace, "ConfigMap.Name", name)
	metaLabels := LabelsForMetadata(FluentdName)
	dataMap := make(map[string]string)
	var err error
	var data string
	switch name {
	case FluentdDaemonSetName + "-" + ConfigName:
		dataMap[EnableAuditLogForwardKey] = strconv.FormatBool(instance.Spec.EnableAuditLoggingForwarding)
		type Data struct {
			Value string `yaml:"fluent.conf"`
		}
		d := Data{}
		data = buildFluentdConfig(instance)
		err = yaml.Unmarshal([]byte(data), &d)
		if err != nil {
			break
		}
		dataMap[FluentdConfigKey] = d.Value
	case FluentdDaemonSetName + "-" + SourceConfigName:
		type DataS struct {
			Value string `yaml:"source.conf"`
		}
		ds := DataS{}
		var result string
		p := strconv.Itoa(defaultHTTPPort)
		result += sourceConfigDataKey + sourceConfigDataHTTP1 + p + sourceConfigDataHTTP2 + filterHTTP
		err = yaml.Unmarshal([]byte(result), &ds)
		if err != nil {
			break
		}
		dataMap[SourceConfigKey] = ds.Value
	case FluentdDaemonSetName + "-" + SplunkConfigName:
		dsplunk := DataSplunk{}
		data = buildFluentdSplunkConfig(instance)
		err = yaml.Unmarshal([]byte(data), &dsplunk)
		if err != nil {
			break
		}
		dataMap[SplunkConfigKey] = dsplunk.Value
	case FluentdDaemonSetName + "-" + QRadarConfigName:
		dq := DataQRadar{}
		data = buildFluentdQRadarConfig(instance)
		err = yaml.Unmarshal([]byte(data), &dq)
		if err != nil {
			break
		}
		dataMap[QRadarConfigKey] = dq.Value
	case FluentdDaemonSetName + "-" + HTTPIngestName:
		p := strconv.Itoa(defaultHTTPPort)
		dataMap[HTTPIngestURLKey] = "https://common-audit-logging" + "." + instance.Namespace + ":" + p + httpPath
	default:
		reqLogger.Info("Unknown ConfigMap name")
	}
	if err != nil {
		reqLogger.Error(err, "Failed to unmarshall data for "+name)
		return nil, err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    metaLabels,
		},
		Data: dataMap,
	}
	return cm, nil
}

func buildFluentdConfig(instance *operatorv1.CommonAudit) string {
	var result = fluentdMainConfigV1Data
	if instance.Spec.Outputs.Splunk.EnableSIEM || instance.Spec.Outputs.Syslog.EnableSIEM {
		result += fluentdOutputConfigV1Data
		if instance.Spec.Outputs.Splunk.EnableSIEM {
			result += yamlLine(2, splunkPlugin, true)
		}
		if instance.Spec.Outputs.Syslog.EnableSIEM {
			result += yamlLine(2, qradarPlugin, true)
		}
		result += yamlLine(1, `</match>`, false)
	}
	return result
}

func buildFluentdSplunkConfig(instance *operatorv1.CommonAudit) string {
	var result = splunkConfigV1Data1
	if instance.Spec.Outputs.Splunk.Host != "" && instance.Spec.Outputs.Splunk.Port != 0 &&
		instance.Spec.Outputs.Splunk.Token != "" {
		result += yamlLine(2, hecHost+instance.Spec.Outputs.Splunk.Host, true)
		result += yamlLine(2, hecPort+strconv.Itoa(int(instance.Spec.Outputs.Splunk.Port)), true)
		result += yamlLine(2, hecToken+instance.Spec.Outputs.Splunk.Token, true)
	} else {
		result += yamlLine(2, hecHost+`SPLUNK_SERVER_HOSTNAME`, true)
		result += yamlLine(2, hecPort+`SPLUNK_PORT`, true)
		result += yamlLine(2, hecToken+`SPLUNK_HEC_TOKEN`, true)
	}
	result += yamlLine(2, protocol+Protocols[instance.Spec.Outputs.Splunk.TLS], false)
	return result + splunkConfigV1Data2
}

func buildFluentdQRadarConfig(instance *operatorv1.CommonAudit) string {
	var result = qRadarConfigV1Data1
	if instance.Spec.Outputs.Syslog.Host != "" && instance.Spec.Outputs.Syslog.Port != 0 &&
		instance.Spec.Outputs.Syslog.Hostname != "" {
		result += yamlLine(2, host+instance.Spec.Outputs.Syslog.Host, true)
		result += yamlLine(2, port+strconv.Itoa(int(instance.Spec.Outputs.Syslog.Port)), true)
		result += yamlLine(2, hostname+instance.Spec.Outputs.Syslog.Hostname, true)
	} else {
		result += yamlLine(2, host+`QRADAR_SERVER_HOSTNAME`, true)
		result += yamlLine(2, port+`QRADAR_PORT_FOR_icp-audit`, true)
		result += yamlLine(2, hostname+`QRADAR_LOG_SOURCE_IDENTIFIER_FOR_icp-audit`, true)
	}
	result += yamlLine(2, tls+strconv.FormatBool(instance.Spec.Outputs.Syslog.TLS), false)
	return result + qRadarConfigV1Data2
}

// UpdateSIEMConfig returns a String
func UpdateSIEMConfig(instance *operatorv1.CommonAudit, found *corev1.ConfigMap) string {
	var newData, d1, d2, d3, d4 string
	if found.Name == FluentdDaemonSetName+"-"+SplunkConfigName {
		if instance.Spec.Outputs.Splunk != (operatorv1.CommonAuditSpecSplunk{}) {
			newData = found.Data[SplunkConfigKey]
			d1 = RegexHecHost.ReplaceAllString(newData, hecHost+instance.Spec.Outputs.Splunk.Host)
			d2 = RegexHecPort.ReplaceAllString(d1, hecPort+strconv.Itoa(int(instance.Spec.Outputs.Splunk.Port)))
			d3 = RegexHecToken.ReplaceAllString(d2, hecToken+instance.Spec.Outputs.Splunk.Token)
			d4 = RegexProtocol.ReplaceAllString(d3, protocol+Protocols[instance.Spec.Outputs.Splunk.TLS])
		}
	} else {
		if instance.Spec.Outputs.Syslog != (operatorv1.CommonAuditSpecSyslog{}) {
			newData = found.Data[QRadarConfigKey]
			d1 = RegexHost.ReplaceAllString(newData, host+instance.Spec.Outputs.Syslog.Host)
			d2 = RegexPort.ReplaceAllString(d1, port+strconv.Itoa(int(instance.Spec.Outputs.Syslog.Port)))
			d3 = RegexHostname.ReplaceAllString(d2, hostname+instance.Spec.Outputs.Syslog.Hostname)
			d4 = RegexTLS.ReplaceAllString(d3, tls+strconv.FormatBool(instance.Spec.Outputs.Syslog.TLS))
		}
	}
	return d4
}

// EqualSIEMConfig returns a Boolean
func EqualSIEMConfig(instance *operatorv1.CommonAudit, found *corev1.ConfigMap) (bool, bool) {
	var key string
	type config struct {
		name  string
		value string
		regex *regexp.Regexp
	}
	var configs = []config{}
	logger := log.WithValues("func", "EqualSIEMConfigs")
	if found.Name == FluentdDaemonSetName+"-"+SplunkConfigName {
		if instance.Spec.Outputs.Splunk != (operatorv1.CommonAuditSpecSplunk{}) {
			key = SplunkConfigKey
			configs = []config{
				{
					name:  "Host",
					value: instance.Spec.Outputs.Splunk.Host,
					regex: RegexHecHost,
				},
				{
					name:  "Port",
					value: strconv.Itoa(int(instance.Spec.Outputs.Splunk.Port)),
					regex: RegexHecPort,
				},
				{
					name:  "Token",
					value: instance.Spec.Outputs.Splunk.Token,
					regex: RegexHecToken,
				},
				{
					name:  "Protocol",
					value: Protocols[instance.Spec.Outputs.Splunk.TLS],
					regex: RegexProtocol,
				},
			}
		}
	} else {
		if instance.Spec.Outputs.Syslog != (operatorv1.CommonAuditSpecSyslog{}) {
			key = QRadarConfigKey
			configs = []config{
				{
					name:  "Host",
					value: instance.Spec.Outputs.Syslog.Host,
					regex: RegexHost,
				},
				{
					name:  "Port",
					value: strconv.Itoa(int(instance.Spec.Outputs.Syslog.Port)),
					regex: RegexPort,
				},
				{
					name:  "Hostname",
					value: instance.Spec.Outputs.Syslog.Hostname,
					regex: RegexHostname,
				},
				{
					name:  "TLS",
					value: strconv.FormatBool(instance.Spec.Outputs.Syslog.TLS),
					regex: RegexTLS,
				},
			}
		}
	}
	var equal = true
	var missing = false
	for _, c := range configs {
		equal = true
		missing = false
		found := c.regex.FindStringSubmatch(found.Data[key])
		if len(found) > 0 {
			value := strings.Split(found[0], " ")
			if len(value) > 1 {
				if value[1] != c.value {
					equal = false
				}
			} else {
				equal = false
				missing = true
			}
		} else {
			equal = false
			missing = true
		}
		if !equal {
			logger.Info(c.name+" incorrect", "Expected", c.value)
			break
		}
	}
	return equal, missing
}

// UpdateMatchTags returns a String
func UpdateMatchTags(found *corev1.ConfigMap) string {
	var data string
	re := regexp.MustCompile(`<match.*>`)
	if found.Name == FluentdDaemonSetName+"-"+SplunkConfigName {
		data = found.Data[SplunkConfigKey]
	} else {
		data = found.Data[QRadarConfigKey]
	}
	return re.ReplaceAllString(removeK8sAudit(data), matchTags)
}

// BuildConfigMap returns a ConfigMap object
func BuildConfigMap(instance *operatorv1alpha1.AuditLogging, name string) (*corev1.ConfigMap, error) {
	reqLogger := log.WithValues("ConfigMap.Namespace", InstanceNamespace, "ConfigMap.Name", name)
	metaLabels := LabelsForMetadata(FluentdName)
	dataMap := make(map[string]string)
	var err error
	switch name {
	case FluentdDaemonSetName + "-" + ConfigName:
		dataMap[EnableAuditLogForwardKey] = strconv.FormatBool(instance.Spec.Fluentd.EnableAuditLoggingForwarding)
		type Data struct {
			Value string `yaml:"fluent.conf"`
		}
		d := Data{}
		err = yaml.Unmarshal([]byte(fluentdMainConfigData), &d)
		if err != nil {
			break
		}
		dataMap[FluentdConfigKey] = d.Value
	case FluentdDaemonSetName + "-" + SourceConfigName:
		type DataS struct {
			Value string `yaml:"source.conf"`
		}
		ds := DataS{}
		var result = sourceConfigDataKey + sourceConfigDataSystemd1
		if instance.Spec.Fluentd.JournalPath != "" {
			result += instance.Spec.Fluentd.JournalPath
		} else {
			result += defaultJournalPath
		}
		result += sourceConfigDataSystemd2
		p := strconv.Itoa(defaultHTTPPort)
		result += sourceConfigDataHTTP1 + p + sourceConfigDataHTTP2 + filterJournal + filterHTTP
		err = yaml.Unmarshal([]byte(result), &ds)
		if err != nil {
			break
		}
		dataMap[SourceConfigKey] = ds.Value
	case FluentdDaemonSetName + "-" + SplunkConfigName:
		dsplunk := DataSplunk{}
		err = yaml.Unmarshal([]byte(splunkConfigData1+splunkDefaults+splunkConfigData2), &dsplunk)
		if err != nil {
			break
		}
		dataMap[SplunkConfigKey] = dsplunk.Value
	case FluentdDaemonSetName + "-" + QRadarConfigName:
		dq := DataQRadar{}
		err = yaml.Unmarshal([]byte(qRadarConfigData1+qRadarDefaults+qRadarConfigData2), &dq)
		if err != nil {
			break
		}
		dataMap[QRadarConfigKey] = dq.Value
	case FluentdDaemonSetName + "-" + HTTPIngestName:
		p := strconv.Itoa(defaultHTTPPort)
		dataMap[HTTPIngestURLKey] = "https://common-audit-logging" + "." + InstanceNamespace + ":" + p + httpPath
	default:
		reqLogger.Info("Unknown ConfigMap name")
	}
	if err != nil {
		reqLogger.Error(err, "Failed to unmarshall data for "+name)
		return nil, err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: InstanceNamespace,
			Labels:    metaLabels,
		},
		Data: dataMap,
	}
	return cm, nil
}

func getConfig(data string) (string, error) {
	var siemConfig string
	// >([^]]+)< retrieves all data between match tags
	regex := `>\n([^]]+)\n<`
	reConfig := regexp.MustCompile(regex)
	matches := reConfig.FindStringSubmatch(data)
	if len(matches) < 2 {
		return data, errors.New("output plugin config misformatted")
	}
	config := matches[1]
	lines := strings.Split(config, "\n")
	var tabs = 1
	for i, line := range lines {
		if i < len(lines)-1 {
			siemConfig += yamlLine(tabs, line, true)
		} else {
			siemConfig += yamlLine(tabs, line, false)
		}
	}
	return siemConfig, nil
}

func removeK8sAudit(data string) string {
	// K8s auditing was deprecated in CS 3.2.4 and removed in CS 3.3
	return strings.Split(data, "<match kube-audit>")[0]
}

func yamlLine(tabs int, line string, newline bool) string {
	spaces := strings.Repeat(`    `, tabs)
	if !newline {
		return spaces + line
	}
	return spaces + line + "\n"
}

// BuildWithSIEMConfigs returns a String and an Error
func BuildWithSIEMConfigs(found *corev1.ConfigMap) (string, error) {
	var result string
	var err error
	var siemConfig string
	if found.Name == FluentdDaemonSetName+"-"+SplunkConfigName {
		siemConfig, err = getConfig(found.Data[SplunkConfigKey])
		if err != nil {
			return siemConfig, err
		}
		ds := DataSplunk{}
		err = yaml.Unmarshal([]byte(splunkConfigData1+"\n"+siemConfig+splunkConfigData2), &ds)
		result = ds.Value
	} else {
		data := removeK8sAudit(found.Data[QRadarConfigKey])
		siemConfig, err = getConfig(data)
		if err != nil {
			return siemConfig, err
		}
		dq := DataQRadar{}
		err = yaml.Unmarshal([]byte(qRadarConfigData1+"\n"+siemConfig+qRadarConfigData2), &dq)
		result = dq.Value
	}
	return result, err
}

// EqualMatchTags returns a Boolean
func EqualMatchTags(found *corev1.ConfigMap) bool {
	logger := log.WithValues("func", "EqualMatchTags")
	var key string
	if found.Name == FluentdDaemonSetName+"-"+SplunkConfigName {
		key = SplunkConfigKey
	} else {
		key = QRadarConfigKey
	}
	re := regexp.MustCompile(`match icp-audit icp-audit\.\*\*`)
	var match = re.FindStringSubmatch(found.Data[key])
	if len(match) < 1 {
		logger.Info("Match tags not equal", "Expected", OutputPluginMatches)
		return false
	}
	return len(match) >= 1
}

// EqualSourceConfig returns a Boolean and a String slice
func EqualSourceConfig(expected *corev1.ConfigMap, found *corev1.ConfigMap) (bool, []string) {
	var ports []string
	var foundPort string
	re := regexp.MustCompile("port [0-9]+")
	var match = re.FindStringSubmatch(found.Data[SourceConfigKey])
	if len(match) < 1 {
		foundPort = ""
	} else {
		foundPort = strings.Split(match[0], " ")[1]
	}
	ports = append(ports, foundPort)
	match = re.FindStringSubmatch(expected.Data[SourceConfigKey])
	expectedPort := strings.Split(match[0], " ")[1]
	return (foundPort == expectedPort), append(ports, expectedPort)
}

// EqualConfig returns a Boolean
func EqualConfig(found *corev1.ConfigMap, expected *corev1.ConfigMap, key string) bool {
	logger := log.WithValues("func", "EqualConfig")
	if !reflect.DeepEqual(found.Data[key], expected.Data[key]) {
		logger.Info("Found config is incorrect", "Key", key, "Found", found.Data[key], "Expected", expected.Data[key])
		return false
	}
	return true
}