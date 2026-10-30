import io,json,unittest
from unittest.mock import patch
from flasher.device import Device

class SSHSetup(unittest.TestCase):
    def test_legacy_api_without_board_can_enable_ssh(self):
        class Opener:
            def __init__(self): self.requests=[]
            def open(self,req,timeout):
                self.requests.append(req)
                replies=[{'model':'Goldshell-KDBox','firmware':'2.1.0'}, {'JWT Token':'test-token'}, {}]
                return io.BytesIO(json.dumps(replies[len(self.requests)-1]).encode())
        opener=Opener()
        with patch('flasher.device.urllib.request.build_opener',return_value=opener):
            Device.enable_ssh('192.168.1.2','test-secret')
        self.assertEqual(len(opener.requests),3)

    def test_reported_wrong_board_does_not_receive_credentials(self):
        class Opener:
            def __init__(self): self.requests=[]
            def open(self,req,timeout):
                self.requests.append(req)
                return io.BytesIO(json.dumps({'model':'Goldshell-KDBox','firmware':'2.1.0','mcbversion':'MCB_V6'}).encode())
        opener=Opener()
        with patch('flasher.device.urllib.request.build_opener',return_value=opener):
            with self.assertRaises(RuntimeError): Device.enable_ssh('192.168.1.2','test-secret')
        self.assertEqual(len(opener.requests),1)

    def test_unsupported_device_never_receives_credentials(self):
        class Opener:
            def __init__(self):self.requests=[]
            def open(self,req,timeout):
                self.requests.append(req)
                return io.BytesIO(json.dumps({'model':'Other','firmware':'2.2.1','mcbversion':'MCB_V5_4'}).encode())
        opener=Opener()
        with patch('flasher.device.urllib.request.build_opener',return_value=opener):
            with self.assertRaises(RuntimeError):Device.enable_ssh('192.168.1.2','test-secret')
        self.assertEqual(len(opener.requests),1)
        self.assertNotIn('test-secret',opener.requests[0].full_url)

    def test_supported_device_authenticates_before_enabling(self):
        class Opener:
            def __init__(self):self.requests=[]
            def open(self,req,timeout):
                self.requests.append(req)
                replies=[{'model':'Goldshell-KDBox','firmware':'2.2.1','mcbversion':'MCB_V5_4'},{'JWT Token':'test-token'},{}]
                return io.BytesIO(json.dumps(replies[len(self.requests)-1]).encode())
        opener=Opener()
        with patch('flasher.device.urllib.request.build_opener',return_value=opener):Device.enable_ssh('192.168.1.2','test-secret')
        self.assertEqual(opener.requests[-1].method,'PUT')
        self.assertTrue(opener.requests[-1].full_url.endswith('/mcb/protect'))
        self.assertEqual(opener.requests[-1].get_header('Authorization'),'Bearer test-token')

    def test_public_address_rejected_before_network_access(self):
        with patch('flasher.device.urllib.request.build_opener') as opener:
            with self.assertRaises(ValueError):Device.enable_ssh('8.8.8.8','test-secret')
            opener.assert_not_called()

    def test_newer_service_hook_requires_owner_login(self):
        import urllib.error
        from urllib.parse import parse_qs, urlparse
        from flasher.device import PROFILE
        class Opener:
            def __init__(self, authorized): self.requests=[]; self.authorized=authorized
            def open(self, req, timeout):
                self.requests.append(req)
                if len(self.requests)==1:
                    return io.BytesIO(json.dumps({'model':'Goldshell-KDBox','firmware':'2.2.3','mcbversion':'MCB_V5_4'}).encode())
                if len(self.requests)==2:
                    return io.BytesIO(json.dumps({'JWT Token':'owner-token'} if self.authorized else {}).encode())
                raise urllib.error.HTTPError(req.full_url,500,'stock hook response',{},None)
        for authorized in [False,True]:
            opener=Opener(authorized)
            with patch.dict(PROFILE,firmwares=['2.2.3']), patch('flasher.device.urllib.request.build_opener',return_value=opener):
                if authorized: Device.enable_ssh('192.168.1.2','owner-password')
                else:
                    with self.assertRaises(RuntimeError): Device.enable_ssh('192.168.1.2','wrong-password')
            self.assertEqual(len(opener.requests),3 if authorized else 2)
            if authorized:
                self.assertEqual(parse_qs(urlparse(opener.requests[1].full_url).query)['password'],['owner-password'])
                self.assertEqual(opener.requests[2].method,'GET')
