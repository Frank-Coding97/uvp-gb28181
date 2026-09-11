"""T01: execute source checks against real, isolated Git repositories."""
import copy
import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True

SPEC = importlib.util.spec_from_file_location('source_lock', Path(__file__).with_name('source_lock.py'))
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


class SourceLockTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='uvp source 中文 ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.repos = {}
        self.lock = {'schema': 1, 'components': {}}
        for name in ('uvp', 'zlm', 'redis'):
            repo = self.root / name
            repo.mkdir()
            git(repo, 'init', '-q')
            git(repo, 'config', 'user.name', 'Fixture')
            git(repo, 'config', 'user.email', 'fixture@example.invalid')
            (repo / 'source.txt').write_text(name)
            (repo / '.gitignore').write_text('secret.yml\n')
            git(repo, 'add', '.')
            git(repo, 'commit', '-qm', 'fixture')
            self.repos[name] = repo
            self.lock['components'][name] = {'revision': git(repo, 'rev-parse', 'HEAD'), 'submodules': {}}

    def test_clean_exact_revisions_pass_without_changes(self):
        result = module.verify(self.lock, self.repos)
        self.assertEqual(set(result), set(self.repos))
        for root in self.repos.values():
            self.assertEqual(git(root, 'status', '--porcelain'), '')

    def test_missing_component_and_symbolic_revision_rejected(self):
        bad = copy.deepcopy(self.lock)
        del bad['components']['redis']
        with self.assertRaises(ValueError):
            module.verify(bad, self.repos)
        for rev in ('HEAD', 'latest', 'a' * 7, ''):
            bad = copy.deepcopy(self.lock)
            bad['components']['uvp']['revision'] = rev
            with self.subTest(rev=rev), self.assertRaises(ValueError):
                module.verify(bad, self.repos)

    def test_mismatched_commit_rejected(self):
        self.lock['components']['uvp']['revision'] = 'a' * 40
        with self.assertRaises(ValueError):
            module.verify(self.lock, self.repos)

    def test_tracked_untracked_and_ignored_inputs_rejected(self):
        root = self.repos['uvp']
        for name in ('source.txt', 'untracked.txt', 'secret.yml'):
            p = root / name
            original = p.read_bytes() if p.exists() else None
            p.write_text('changed')
            with self.subTest(name=name), self.assertRaises(ValueError):
                module.verify(self.lock, self.repos)
            if original is None:
                p.unlink()
            else:
                p.write_bytes(original)

    def test_submodule_revision_and_dirty_state_checked(self):
        root = self.repos['zlm']
        git(root, '-c', 'protocol.file.allow=always', 'submodule', 'add', '-q', str(self.repos['redis']), 'vendor/redis')
        git(root, 'commit', '-qam', 'submodule')
        self.lock['components']['zlm']['revision'] = git(root, 'rev-parse', 'HEAD')
        with self.assertRaises(ValueError):
            module.verify(self.lock, self.repos)
        self.lock['components']['zlm']['submodules']['vendor/redis'] = git(self.repos['redis'], 'rev-parse', 'HEAD')
        module.verify(self.lock, self.repos)
        (root / 'vendor/redis/source.txt').write_text('modified')
        with self.assertRaises(ValueError):
            module.verify(self.lock, self.repos)

    def test_wrong_tag_target_rejected(self):
        git(self.repos['redis'], 'tag', '7.2.fixture')
        self.lock['components']['redis']['tag'] = '7.2.fixture'
        module.verify(self.lock, self.repos)
        self.lock['components']['redis']['tag'] = 'not-present'
        with self.assertRaises(ValueError):
            module.verify(self.lock, self.repos)

    def test_invalid_submodule_path_rejected(self):
        for path in ('../outside', '/absolute', 'vendor/../outside', 'C:\\outside'):
            bad = copy.deepcopy(self.lock)
            bad['components']['zlm']['submodules'][path] = 'a' * 40
            with self.subTest(path=path), self.assertRaises(ValueError):
                module.verify(bad, self.repos)

    def test_cli_failure_is_nonzero_and_does_not_claim_verified(self):
        import json
        lockfile = self.root / 'lock.json'
        lockfile.write_text(json.dumps(self.lock))
        (self.repos['uvp'] / 'untracked').touch()
        args = ['python3', str(Path(__file__).with_name('source_lock.py')), '--lock', str(lockfile)]
        for name, path in self.repos.items():
            args += ['--' + name, str(path)]
        result = subprocess.run(args, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('source_verified', result.stdout)


if __name__ == '__main__':
    unittest.main()
