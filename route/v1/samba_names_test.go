package v1

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/IceWhaleTech/CasaOS/service"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"github.com/labstack/echo/v4"
)

type shareNameTestService struct {
	service.SharesService
	existing, created []model2.SharesDBModel
}

func (s *shareNameTestService) GetSharesByPath(path string) []model2.SharesDBModel {
	for _, share := range s.existing {
		if share.Path == path {
			return []model2.SharesDBModel{share}
		}
	}
	return nil
}
func (s *shareNameTestService) GetSharesByName(name string) []model2.SharesDBModel {
	for _, share := range s.existing {
		if strings.EqualFold(share.Name, name) {
			return []model2.SharesDBModel{share}
		}
	}
	return nil
}
func (s *shareNameTestService) CreateShare(share model2.SharesDBModel) error {
	s.created = append(s.created, share)
	return nil
}

type shareNameTestRepository struct {
	service.Repository
	shares *shareNameTestService
}

func (r shareNameTestRepository) Shares() service.SharesService { return r.shares }

func TestShareCreationRejectsExistingAndBatchNameCollisions(t *testing.T) {
	previous := service.MyService
	defer func() { service.MyService = previous }()
	root := t.TempDir()
	a, b := filepath.Join(root, "A", "Photos"), filepath.Join(root, "B", "Photos")
	for _, dir := range []string{a, b} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		existing        []model2.SharesDBModel
		requested       []model.Shares
		status, created int
	}{
		{[]model2.SharesDBModel{{Name: "PHOTOS", Path: a}}, []model.Shares{{Path: b}}, 400, 0},
		{nil, []model.Shares{{Path: a}, {Path: b}}, 400, 0},
		{nil, []model.Shares{{Path: a, Name: "First"}, {Path: b, Name: "Second"}}, 200, 2},
		{nil, []model.Shares{{Path: a, Name: "bad/name"}}, 400, 0},
	} {
		s := &shareNameTestService{existing: test.existing}
		service.MyService = shareNameTestRepository{shares: s}
		body, err := json.Marshal(test.requested)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/v1/samba/shares", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e := echo.New()
		c := e.NewContext(r, w)
		if err := PostSambaSharesCreate(c); err != nil {
			e.HTTPErrorHandler(err, c)
		}
		if w.Code != test.status || len(s.created) != test.created {
			t.Fatal(w.Code, w.Body.String(), s.created)
		}
		if test.created == 2 && (s.created[0].Name != "First" || s.created[1].Name != "Second") {
			t.Fatal("explicit names lost", s.created)
		}
	}
}
