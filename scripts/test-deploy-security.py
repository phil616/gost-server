#!/usr/bin/env python3
"""Verify deployment isolation with fake Docker; never touch real containers."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


FAKE_DOCKER = r'''#!/usr/bin/env python3
import json
import os
import sys
with open(os.environ['DEPLOY_TRACE'], 'a') as trace:
    trace.write(json.dumps(sys.argv[1:]) + '\n')
if sys.argv[1] == 'inspect':
    print('running healthy')
'''


def main():
    root = Path(__file__).resolve().parent
    with tempfile.TemporaryDirectory(prefix='altasci-deploy-security-') as directory:
        temp = Path(directory)
        shutil.copyfile(root / 'deploy-docker.sh', temp / 'deploy-docker.sh')
        (temp / 'init-docker.sh').write_text('#!/usr/bin/env bash\nexit 0\n')
        (temp / 'docker').write_text(FAKE_DOCKER)
        (temp / 'docker').chmod(0o700)
        (temp / 'test.env').write_text('DATABASE_URL=isolated-test-only\n')
        trace = temp / 'trace.jsonl'
        subprocess.run(
            ['bash', str(temp / 'deploy-docker.sh'), 'isolated-test-image', str(temp / 'test.env')],
            env=dict(os.environ, PATH=str(temp) + os.pathsep + os.environ['PATH'],
                     DEPLOY_TRACE=str(trace), ALTASCI_DEPLOY_LOCK_DIR=str(temp)),
            check=True, timeout=10,
        )
        events = [json.loads(line) for line in trace.read_text().splitlines()]
        migrations = [e for e in events if e[0] == 'run' and e[-1] == 'migrate']
        applications = [e for e in events if e[:2] == ['run', '-d']]
        assert len(migrations) == len(applications) == 1
        assert events.index(migrations[0]) < events.index(applications[0])
        application = applications[0]
        ports = [application[i + 1] for i, arg in enumerate(application) if arg == '-p']
        assert ports == ['127.0.0.1:18080:18080', '127.0.0.1:18081:18081'], ports
        assert '--read-only' in application
        assert application[application.index('--cap-drop') + 1] == 'ALL'
        assert application[application.index('--security-opt') + 1] == 'no-new-privileges'
        print('PASS: deployment keeps plaintext ports on loopback and retains container isolation')


if __name__ == '__main__':
    main()
