// Copyright 2017 Hewlett Packard Enterprise Development LP
//
//    Licensed under the Apache License, Version 2.0 (the "License"); you may
//    not use this file except in compliance with the License. You may obtain
//    a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
//    WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
//    License for the specific language governing permissions and limitations
//    under the License.

package main

import (
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/monasca/golang-monascaclient/monascaclient"
	"github.com/monasca/golang-monascaclient/monascaclient/models"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

var webhookTriggered bool = false
var testsSucceeded int = 0

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Received WEBHOOK")
	webhookTriggered = true
}

func getToken(authOptions *gophercloud.AuthOptions) (string, error) {
	openstackProvider, err := openstack.AuthenticatedClient(*authOptions)
	if err != nil {
		return "", err
	}
	return openstackProvider.TokenID, nil
}

func initializeMonascaClient(token, monascaURL string) {
	timeoutSetting := os.Getenv("TIMEOUT")
	var timeout int
	if timeoutSetting == "" {
		timeout = 5
	} else {
		var err error
		timeout, err = strconv.Atoi(timeoutSetting)
		if err != nil {
			fmt.Printf("Error converting TIMEOUT environment varible to int -  %s. Defaulting "+
				"timeout to 5 seconds", err.Error())
			timeout = 5
		}
	}
	monascaclient.SetBaseURL(monascaURL)
	monascaclient.SetTimeout(timeout)
	headers := http.Header{}
	headers.Add("X-Auth-Token", token)
	monascaclient.SetHeaders(headers)
}

func testMeasurementsFlowing() {
	//mergeMetrics := false
	metricName := "pod.cpu.total_time_sec"
	startTime := time.Now()
	startTime = startTime.Add(-3 * time.Minute)
	groupBy := "*"
	measurementQuery := models.MeasurementQuery{Name: &metricName, StartTime: &startTime, GroupBy: &groupBy}
	measurements, err := monascaclient.GetMeasurements(&measurementQuery)
	if err != nil {
		fmt.Printf("FAILED - Error getting measurements from API test failed %s\n", err.Error())
		return
	}
	if len(measurements.Elements) == 0 {
		fmt.Println("FAILED - No current measurements found for pod.cpu.total_time_sec")
		return
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
}

func testCreateNotification(webhookAddress string) string {
	name := "smoke_test_notification"
	notificationType := "webhook"
	requestBody := models.NotificationRequestBody{Name: &name, Type: &notificationType, Address: &webhookAddress}
	notificationResponse, err := monascaclient.CreateNotificationMethod(&requestBody)
	if err != nil {
		fmt.Printf("FAILED - Error creating notification method %s\n", err.Error())
		return ""
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
	return notificationResponse.ID
}

func testCreateAlarmDefinition(notificationID string) string {
	name := "smoke_test_alarm"
	expression := "smoke_test_metric>0"
	alarmDefActions := []string{notificationID}
	requestBody := models.AlarmDefinitionRequestBody{Name: &name, Expression: &expression, AlarmActions: &alarmDefActions, UndeterminedActions: &alarmDefActions, OkActions: &alarmDefActions}
	alarmDefinitionResponse, err := monascaclient.CreateAlarmDefinition(&requestBody)
	if err != nil {
		fmt.Printf("FAILED - Error creating alarm definition %s\n", err.Error())
		return ""
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
	return alarmDefinitionResponse.ID
}

func testCreateMetric(value float64) {
	now := time.Now().Round(time.Millisecond).UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
	name := "smoke_test_metric"
	err := monascaclient.CreateMetric(nil, &models.MetricRequestBody{Name: &name, Value: &value, Timestamp: &now})
	if err != nil {
		fmt.Printf("FAILED - Error creating metric %s\n", err.Error())
		return
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
}

func testWebhookTrigger() {
	// Wait 5 minutes for webhook to trigger
	for i := 0; i < 60; i++ {
		if webhookTriggered {
			fmt.Println("SUCCESS")
			testsSucceeded++
			return
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Println("FAILED - Did not recieve webhook")
}

func cleanup(alarmDefinitionID, notificationID string) {
	cleanupStatus := true
	if alarmDefinitionID != "" {
		err := monascaclient.DeleteAlarmDefinition(alarmDefinitionID)
		if err != nil {
			fmt.Printf("FAILED - Error alarm definition - %s\n", err.Error())
			cleanupStatus = false
		}
	}
	if notificationID != "" {
		err := monascaclient.DeleteNotificationMethod(notificationID)
		if err != nil {
			fmt.Printf("FAILED - Error deleting alarm definition - %s\n", err.Error())
			cleanupStatus = false
		}
	}
	if cleanupStatus {
		testsSucceeded++
		fmt.Println("SUCCESS")
	}
}

func cleanupPreviousRun() {
	notificationName := "smoke_test_notification"
	// Get notification method
	notificationMethods, err := monascaclient.GetNotificationMethods(&models.NotificationQuery{})
	if err != nil {
		fmt.Printf("Error getting notification methods to delete potential left over notification method "+
			"from previous runs - %s\n", err.Error())
	} else {
		notificationID := ""
		for _, notificationMethod := range notificationMethods.Elements {
			if notificationMethod.Name == notificationName {
				notificationID = notificationMethod.ID
			}
		}
		if notificationID != "" {
			err := monascaclient.DeleteNotificationMethod(notificationID)
			if err != nil {
				fmt.Printf("Error deleting notification method - %s\n", err.Error())
			}
		}
	}
	alarmDefinitionName := "smoke_test_alarm"
	alarmDefinitions, err := monascaclient.GetAlarmDefinitions(&models.AlarmDefinitionQuery{Name: &alarmDefinitionName})
	if err != nil {
		fmt.Printf("Error getting alarm definitions to delete potential left over alarm definition "+
			"from previous runs - %s\n", err.Error())
	} else {
		alarmDefinitionID := ""
		for _, alarmDefinition := range alarmDefinitions.Elements {
			if alarmDefinition.Name == alarmDefinitionName {
				alarmDefinitionID = alarmDefinition.ID
			}
		}
		if alarmDefinitionID != "" {
			err := monascaclient.DeleteAlarmDefinition(alarmDefinitionID)
			if err != nil {
				fmt.Printf("Error deleting alarm definition - %s\n", err.Error())
			}
		}
	}
}

func main() {
	// Initialize keystone client and get Monasca endpoint
	keystoneOpts, err := openstack.AuthOptionsFromEnv()
	testsRun := 0

	if err != nil {
		fmt.Printf("ERROR setting up keystone client - %s", err.Error())
		os.Exit(1)
	}

	monascaURL := os.Getenv("MONASCA_URL")

	if monascaURL == "" {
		fmt.Println("Monasca URL environment variable must be set")
		os.Exit(1)
	}

	keystoneToken, err := getToken(&keystoneOpts)

	if err != nil {
		fmt.Printf("ERROR getting keystone token - %s", err.Error())
		os.Exit(1)
	}

	initializeMonascaClient(keystoneToken, monascaURL)

	// Cleanup potential smoke test alarm definition and notification method from previous runs
	cleanupPreviousRun()

	fmt.Println("TEST MEASUREMENTS FLOWING")
	testMeasurementsFlowing()
	fmt.Println()
	testsRun++

	// Set Up Webhook server for notifications
	webhookIP := os.Getenv("WEBHOOK_IP")
	if webhookIP == "" {
		webhookIP = "127.0.0.1"
	}
	webhookAddress := "http://" + webhookIP + ":8080"
	http.HandleFunc("/", handleWebhook)
	go http.ListenAndServe(":8080", nil)

	fmt.Println("TEST NOTIFICATION CREATION")
	notificationID := testCreateNotification(webhookAddress)
	fmt.Println()
	testsRun++

	fmt.Println("TEST ALARM DEFINITION CREATION")
	alarmDefinitionID := testCreateAlarmDefinition(notificationID)
	fmt.Println()
	testsRun++

	fmt.Println("TEST METRIC CREATION")
	testCreateMetric(1)
	fmt.Println()
	testsRun++

	fmt.Println("TEST WEBHOOK TRIGGERED")
	testWebhookTrigger()
	fmt.Println()
	testsRun++

	fmt.Println("TEST CLEANUP")
	cleanup(alarmDefinitionID, notificationID)
	fmt.Println()
	testsRun++

	if testsSucceeded != testsRun {
		fmt.Printf("Smoke Tests Failed. %d/%d passed\n", testsSucceeded, testsRun)
		os.Exit(1)
	} else {
		fmt.Println("All smoke tests passed successfully!!!")
	}
}
