import io,json,unittest
from unittest.mock import patch
from flasher.device import Device

class SSHSetup(unittest.TestCase):
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
