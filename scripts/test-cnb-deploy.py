#!/usr/bin/env python3
"""Exercise the CNB SSH bootstrap locally with fake Docker/baoctl commands."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

import yaml


FAKE_COMMAND = r'''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

command = Path(sys.argv[0]).name
arguments = sys.argv[1:]
with open(os.environ['COMMAND_TRACE'], 'a') as trace:
    trace.write(json.dumps([command, *arguments]) + '\n')
if command == 'baoctl':
    sys.exit(0)
action = arguments[0]
if action == 'login':
    assert arguments[1:] == ['--username', 'existing-deploy-user', '--password-stdin', 'registry.example']
    Path(os.environ['TOKEN_CAPTURE']).write_bytes(sys.stdin.buffer.read())
    sys.exit(42 if os.environ.get('FAIL_LOGIN') == '1' else 0)
if action == 'pull':
    assert Path(os.environ['TOKEN_CAPTURE']).exists(), 'pull attempted without registry login'
    sys.exit(43 if os.environ.get('FAIL_PULL') == '1' else 0)
if action == 'create':
    print('fixture-source-container')
elif action == 'cp':
    destination = Path(arguments[-1])
    destination.joinpath('deploy-docker.sh').write_text(
        '#!/usr/bin/env bash\nset -eu\nprintf "%s\\n" "$@" > "$DEPLOY_CAPTURE"\n'
    )
elif action != 'rm':
    raise AssertionError('Unexpected Docker operation: ' + action)
'''


def main():
    root = Path(__file__).resolve().parent.parent
    configuration = yaml.safe_load((root / '.cnb.yml').read_text())
    stage = configuration['main']['push'][0]['stages'][-1]
    assert stage['name'] == '通过 ssh 部署后端'

    with tempfile.TemporaryDirectory(prefix='altasci-cnb-auth-test-') as directory:
        temp = Path(directory)
        marker = temp / 'must-not-exist'
        # Quoted heredoc input must preserve shell metacharacters as data.
        token = 'test-\'"-$literal-`uname`-$(touch ' + str(marker) + ')'
        variables = {
            'CNB_TOKEN_USER_NAME': 'existing-deploy-user',
            'DEVOPS_TASKMGR_CNB_DEPLOY_TOKEN': token,
            'CNB_DOCKER_REGISTRY': 'registry.example',
            'CNB_REPO_SLUG_LOWERCASE': 'org/project',
            'CNB_COMMIT': 'a' * 40,
        }

        def replace(match):
            return variables[match.group(1)]

        script = re.sub(r'(?<!\\)\$\{([A-Z_]+)\}', replace, stage['settings']['script'])
        script = script.replace('\\$', '$')
        subprocess.run(['sh', '-n'], input=script, text=True, check=True)
        for name in ('docker', 'baoctl'):
            command = temp / name
            command.write_text(FAKE_COMMAND)
            command.chmod(0o700)

        for failure in (None, 'FAIL_LOGIN', 'FAIL_PULL'):
            trace = temp / 'trace.jsonl'
            capture = temp / 'token'
            deployment = temp / 'deployment'
            for path in (trace, capture, deployment):
                path.unlink(missing_ok=True)
            environment = dict(
                os.environ,
                PATH=str(temp) + os.pathsep + os.environ['PATH'],
                COMMAND_TRACE=str(trace), TOKEN_CAPTURE=str(capture), DEPLOY_CAPTURE=str(deployment),
                FAIL_LOGIN='0', FAIL_PULL='0',
            )
            if failure:
                environment[failure] = '1'
            result = subprocess.run(['sh'], input=script, text=True, capture_output=True,
                                    env=environment, timeout=15)
            events = [json.loads(line) for line in trace.read_text().splitlines()]
            assert events[0][0:2] == ['docker', 'login']
            assert capture.read_text() == token + '\n'
            assert not marker.exists(), 'Registry token was executed as shell code'
            assert token not in result.stdout + result.stderr
            if failure:
                assert result.returncode != 0
                expected = ['login'] if failure == 'FAIL_LOGIN' else ['login', 'pull']
                assert [event[1] for event in events] == expected
                assert not deployment.exists()
                print('PASS: authentication/pull failure stops before any deployment mutation')
            else:
                assert result.returncode == 0, result.stderr
                image = 'registry.example/org/project:' + 'a' * 40
                assert deployment.read_text().splitlines() == [
                    image, '/run/baoctl/altasci-tunnel.env', 'altasci-server-data', 'bridge',
                    image + '-init-client',
                ]
                print('PASS: original credentials authenticate before pull; token stays literal')
                print('PASS: initialization client uses the same CNB registry as the release')


if __name__ == '__main__':
    main()
