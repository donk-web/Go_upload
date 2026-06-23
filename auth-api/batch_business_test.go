package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBatchBusinessClientQueryResident(t *testing.T) {
	httpClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			var body string
			switch r.URL.Path {
			case "/apis/yqfk-population/rhr/getRhrBasicInfoList":
				body = `{"code":0,"data":{"list":[{"id":"resident-1","idNumber":"440101199001011234","name":"张三"}]}}`
			case "/apis/yqfk-population/rhr/getViewLogList/resident-1":
				body = `{"code":0,"data":[{"viewTime":"2026-06-18 10:00:00","viewOrgName":"测试医院","departmentName":"全科","viewUserName":"李医生","accessChannel":"1"}]}`
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
			return jsonResponse(body), nil
		}),
	}

	client := &batchBusinessClient{
		baseURL: "https://example.test",
		client:  httpClient,
	}
	records, err := client.QueryResident(context.Background(), "test-token", "440101199001011234")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Name != "张三" || records[0].AccessChannel != "社区通" {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestBatchBusinessClientReturnsNotFound(t *testing.T) {
	client := &batchBusinessClient{
		baseURL: "https://example.test",
		client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return jsonResponse(`{"code":0,"data":{"list":[]}}`), nil
			}),
		},
	}
	_, err := client.QueryResident(context.Background(), "test-token", "440101199001011234")
	if err != errBatchResidentNotFound {
		t.Fatalf("err = %v, want %v", err, errBatchResidentNotFound)
	}
}

func TestBatchBusinessClientQueryResidentNewMergesAllArchives(t *testing.T) {
	httpClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body string
			switch r.URL.Path {
			case "/apis/yqfk-population/basicHealthPop/getYqfkZhcxList":
				body = `{"code":0,"data":{"list":[{"identityNumEncrypt":"encrypted-id","realIdentityNum":"440101199001011234","realName":"张三"}]}}`
			case "/apis/yqfk-population/rhr/getBasicInfoListForApp":
				body = `{"code":0,"data":[{"id":"archive-1","name":"张*"},{"id":"archive-2","name":"张*"}]}`
			case "/apis/yqfk-population/rhr/getViewLogList/archive-1":
				body = `{"code":0,"data":[{"viewTime":"2026-06-18 10:00:00","viewOrgName":"医院A","departmentName":"全科","viewUserName":"李医生","accessChannel":"1"}]}`
			case "/apis/yqfk-population/rhr/getViewLogList/archive-2":
				body = `{"code":0,"data":[{"viewTime":"2026-06-19 11:00:00","viewOrgName":"医院B","departmentName":"内科","viewUserName":"王医生","accessChannel":"2"},{"viewTime":"2026-06-18 10:00:00","viewOrgName":"医院A","departmentName":"全科","viewUserName":"李医生","accessChannel":"1"}]}`
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
			return jsonResponse(body), nil
		}),
	}

	client := &batchBusinessClient{
		baseURL: "https://example.test",
		client:  httpClient,
	}
	records, err := client.QueryResidentWithMethod(
		context.Background(),
		"test-token",
		"440101199001011234",
		batchQueryMethodNew,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Name != "张三" || records[1].AccessChannel != "医院HIS系统" {
		t.Fatalf("records = %#v", records)
	}
	if records[0].Index != 1 || records[1].Index != 2 {
		t.Fatalf("record indexes = %d, %d", records[0].Index, records[1].Index)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}
