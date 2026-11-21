package apicontrollers

import "testing"

func TestValidPushEndpoint(t *testing.T) {
	for endpoint, want := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc:def":             true,
		"https://updates.push.services.mozilla.com/wpush/v2/gAAA": true,
		"https://web.push.apple.com/QGuQyavXutnMH":                true,
		"https://wns2-par02p.notify.windows.com/w/?token=BQYAAAB": true,
		"https://fcm.googleapis.com:443/fcm/send/abc":             true,
		"http://fcm.googleapis.com/fcm/send/abc":                  false, // not https
		"https://fcm.googleapis.com:8443/x":                       false,
		"https://user@fcm.googleapis.com/x":                       false,
		"https://evilfcm.googleapis.com.example.com/x":            false,
		"https://notfcm.googleapis.com/x":                         false, // a suffix without the dot
		"https://storage.googleapis.com/bucket/x":                 false, // another service of the domain
		"https://db:5432/":                                        false,
		"https://169.254.169.254/latest/meta-data":                false,
		"https://localhost/x":                                     false,
		"not a url":                                               false,
		"":                                                        false,
	} {
		if got := validPushEndpoint(endpoint); got != want {
			t.Errorf("%q: %v, want %v", endpoint, got, want)
		}
	}
}

func TestValidPushKey(t *testing.T) {
	p256dh := "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM"
	auth := "tBHItJI5svbpez7KI4CCXg"
	if !validPushKey(p256dh, 65) || !validPushKey(auth, 16) || !validPushKey(auth+"==", 16) {
		t.Error("valid keys refused")
	}
	if validPushKey(auth, 65) || validPushKey("not base64!", 16) || validPushKey("", 16) {
		t.Error("invalid keys accepted")
	}
}
