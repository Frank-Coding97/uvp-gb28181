package manscdp

import "testing"

func TestCatalogParentalPresence(t *testing.T) {
	for _, declaration := range []string{"", "<Parental>0</Parental>", "<Parental>1</Parental>"} {
		body := []byte(`<Response><CmdType>Catalog</CmdType><DeviceList><Item><DeviceID>camera</DeviceID>` + declaration + `</Item></DeviceList></Response>`)
		response, err := ParseCatalogResponse(body)
		if err != nil {
			t.Fatal(err)
		}
		item := response.DeviceList.Items[0]
		if item.ParentalPresent != (declaration != "") {
			t.Fatalf("presence for %q: %+v", declaration, item)
		}
		if declaration == "<Parental>1</Parental>" && item.Parental != 1 {
			t.Fatalf("lost Parental value: %+v", item)
		}
	}
}
