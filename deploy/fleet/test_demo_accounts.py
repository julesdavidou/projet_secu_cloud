import contextlib
import io
import json
from pathlib import Path
import runpy
import sys
import tempfile
import unittest
from unittest.mock import patch

import secure_client as sc


class DemoAccountsTests(unittest.TestCase):
 def test_retry_preserves_password_and_owner_and_never_prints_password(self):
  main=runpy.run_path(str(sc.HERE/'prepare-demo-accounts.py'))['main']
  owner='client-00000000-0000-0000-0000-000000000001'
  with tempfile.TemporaryDirectory() as directory:
   argv=['prepare-demo-accounts.py','--clients','pc-2','--output',directory]
   def response(*args,**kw):return io.BytesIO(json.dumps({'status':'AUTHENTICATED','client_id':owner}).encode())
   log=io.StringIO()
   with patch.object(sys,'argv',argv),patch('urllib.request.urlopen',side_effect=response),patch('secrets.token_urlsafe',return_value='synthetic-private-password') as random,contextlib.redirect_stdout(log):
    main();main()
   random.assert_called_once()
   account=sc.read(Path(directory)/'pc-2.json')
   self.assertEqual(account['password'],'synthetic-private-password')
   self.assertEqual(account['owner_id'],owner)
   self.assertNotIn(account['password'],log.getvalue())
   with patch.object(sys,'argv',argv),patch('urllib.request.urlopen',side_effect=lambda *a,**k:io.BytesIO(b'{"status":"AUTHENTICATED","client_id":"different"}')),self.assertRaisesRegex(ValueError,'identity differs'):main()
   self.assertEqual(sc.read(Path(directory)/'pc-2.json'),account)

 def test_plain_http_accounts_are_restricted_to_controller_loopback(self):
  main=runpy.run_path(str(sc.HERE/'prepare-demo-accounts.py'))['main']
  with patch.object(sys,'argv',['prepare-demo-accounts.py','--clients','pc-2','--portal','http://192.168.2.51:19690','--output','unused']),self.assertRaisesRegex(ValueError,'loopback'):main()
