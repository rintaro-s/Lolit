using System.Drawing;
using System.Windows.Forms;

namespace LolitSolidWorksAddin
{
    /// <summary>Prompts for Lolit account credentials (not Git/Gitea credentials -- Lolit's own lightweight accounts).</summary>
    internal class LoginDialog : Form
    {
        private readonly TextBox _username = new TextBox();
        private readonly TextBox _password = new TextBox { UseSystemPasswordChar = true };
        private readonly Label _error = new Label { ForeColor = Color.Firebrick, AutoSize = false };

        public string Username => _username.Text.Trim();
        public string Password => _password.Text;

        public LoginDialog()
        {
            Text = "Lolit にログイン";
            Width = 340;
            Height = 240;
            FormBorderStyle = FormBorderStyle.FixedDialog;
            MaximizeBox = false;
            MinimizeBox = false;
            StartPosition = FormStartPosition.CenterScreen;

            var lblUser = new Label { Text = "ユーザー名", Left = 16, Top = 16, Width = 280 };
            _username.SetBounds(16, 36, 280, 24);
            var lblPass = new Label { Text = "パスワード", Left = 16, Top = 68, Width = 280 };
            _password.SetBounds(16, 88, 280, 24);
            _error.SetBounds(16, 118, 280, 40);

            var ok = new Button { Text = "ログイン", DialogResult = DialogResult.OK, Left = 132, Top = 165, Width = 80 };
            var cancel = new Button { Text = "キャンセル", DialogResult = DialogResult.Cancel, Left = 216, Top = 165, Width = 80 };
            AcceptButton = ok;
            CancelButton = cancel;

            Controls.AddRange(new Control[] { lblUser, _username, lblPass, _password, _error, ok, cancel });
        }

        public void ShowError(string message) => _error.Text = message;
    }
}
