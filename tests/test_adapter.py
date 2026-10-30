import unittest
from unittest.mock import Mock
from flasher.device import Device

class Compatibility(unittest.TestCase):
    def test_unsupported_firmware_never_reaches_mutation(self):
        device = Device.__new__(Device)
        device.api = Mock(return_value={'model':'Goldshell-KDBox','firmware':'2.2.3','mcbversion':'MCB_V5_4'})
        device.command = Mock()
        with self.assertRaises(RuntimeError): device.inspect()
        device.command.assert_not_called()

    def test_different_model_is_rejected(self):
        device = Device.__new__(Device)
        device.api = Mock(return_value={'model':'Goldshell-HSBox','firmware':'2.2.1','mcbversion':'MCB_V5_4'})
        with self.assertRaises(RuntimeError): device.inspect()

    def test_supported_board(self):
        device = Device.__new__(Device)
        device.api = Mock(return_value={'model':'Goldshell-KDBox','firmware':'2.2.1','mcbversion':'MCB_V5_4'})
        device.command = Mock(return_value='armv7l\n')
        self.assertEqual(device.inspect()['firmware'],'2.2.1')

if __name__=='__main__':unittest.main()
