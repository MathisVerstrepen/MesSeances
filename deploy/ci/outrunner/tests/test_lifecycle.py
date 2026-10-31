import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
NAME = 'messeances-vps-deadbeef'


def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class CleanupTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.module = load('cleanup')
        self.module.BASE = self.root

    def test_removes_only_owned_overlays(self):
        owned = self.root / (NAME + '.qcow2')
        owned.touch()
        unrelated = self.root / 'production.qcow2'
        unrelated.touch()
        malformed = self.root / 'messeances-vps-not-a-uuid.qcow2'
        malformed.touch()
        with patch.object(self.module, 'virsh', return_value=''):
            self.module.main()
        self.assertFalse(owned.exists())
        self.assertTrue(unrelated.exists())
        self.assertTrue(malformed.exists())

    def test_preserves_overlay_when_domain_still_exists(self):
        owned = self.root / (NAME + '.qcow2')
        owned.touch()
        with patch.object(self.module, 'virsh', side_effect=[NAME, '', NAME]):
            self.module.main()
        self.assertTrue(owned.exists())

    def test_destroy_failure_preserves_disk(self):
        owned = self.root / (NAME + '.qcow2')
        owned.touch()
        error = subprocess.CalledProcessError(1, ['virsh'])
        with patch.object(self.module, 'virsh', side_effect=[NAME, error]):
            with self.assertRaises(subprocess.CalledProcessError):
                self.module.main()
        self.assertTrue(owned.exists())

    def test_rejects_symlink_without_touching_target(self):
        target = self.root / 'production.qcow2'
        target.write_text('preserved')
        (self.root / (NAME + '.qcow2')).symlink_to(target)
        with patch.object(self.module, 'virsh', return_value=''):
            with self.assertRaises(RuntimeError):
                self.module.main()
        self.assertEqual(target.read_text(), 'preserved')

    def test_does_not_destroy_unrelated_domains(self):
        with patch.object(self.module, 'virsh', return_value='production\nmesseances-vps-other\n') as run:
            self.module.main()
        self.assertTrue(all(call.args[0] == 'list' for call in run.call_args_list))


class WatchdogTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.module = load('watchdog')
        self.module.LEASES = Path(self.tmp.name)
        self.module.NVRAM = Path(self.tmp.name) / 'nvram'
        self.module.NVRAM.mkdir()

    def test_first_observation_does_not_stop_new_vm(self):
        with patch.object(self.module, 'virsh', side_effect=[NAME, 'uuid-1', NAME]) as run:
            self.module.main()
        self.assertFalse(any(call.args[0] == 'destroy' for call in run.call_args_list))

    def test_expired_vm_is_destroyed_by_verified_uuid(self):
        (self.module.LEASES / NAME).write_text(json.dumps({'uuid': 'uuid-1', 'created_at': 0}))
        with patch.object(self.module, 'virsh', side_effect=[NAME, 'uuid-1', 'uuid-1', '', '']) as run:
            with patch.object(self.module, 'restart_controller') as restart:
                self.module.main()
        self.assertTrue(any(call.args == ('destroy', 'uuid-1') for call in run.call_args_list))
        restart.assert_called_once()

    def test_identity_change_prevents_disposal(self):
        (self.module.LEASES / NAME).write_text(json.dumps({'uuid': 'uuid-1', 'created_at': 0}))
        with patch.object(self.module, 'virsh', side_effect=[NAME, 'uuid-1', 'uuid-2']) as run:
            with self.assertRaises(RuntimeError):
                self.module.main()
        self.assertFalse(any(call.args[0] == 'destroy' for call in run.call_args_list))

    def test_cleans_only_inactive_owned_uefi_stores(self):
        owned = self.module.NVRAM / (NAME + '_VARS.fd')
        owned.touch()
        unrelated = self.module.NVRAM / 'production_VARS.fd'
        unrelated.touch()
        with patch.object(self.module, 'virsh', return_value=''):
            self.module.main()
        self.assertFalse(owned.exists())
        self.assertTrue(unrelated.exists())


if __name__ == '__main__':
    unittest.main()
