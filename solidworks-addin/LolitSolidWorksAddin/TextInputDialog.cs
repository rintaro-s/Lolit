using System.Windows.Forms;

namespace LolitSolidWorksAddin
{
    /// <summary>Small reusable "type a line of text" prompt (e.g. an optional save note).</summary>
    internal class TextInputDialog : Form
    {
        private readonly TextBox _input = new TextBox();

        public string Value => _input.Text.Trim();

        public TextInputDialog(string title, string prompt, string placeholder = "")
        {
            Text = title;
            Width = 380;
            Height = 160;
            FormBorderStyle = FormBorderStyle.FixedDialog;
            MaximizeBox = false;
            MinimizeBox = false;
            StartPosition = FormStartPosition.CenterScreen;

            var label = new Label { Text = prompt, Left = 16, Top = 16, Width = 340 };
            _input.SetBounds(16, 40, 340, 24);
            if (!string.IsNullOrEmpty(placeholder))
            {
                _input.Text = placeholder;
                _input.ForeColor = System.Drawing.Color.Gray;
                _input.Enter += (s, e) => { if (_input.Text == placeholder) { _input.Text = ""; _input.ForeColor = System.Drawing.SystemColors.WindowText; } };
            }

            var ok = new Button { Text = "OK", DialogResult = DialogResult.OK, Left = 172, Top = 80, Width = 80 };
            var cancel = new Button { Text = "キャンセル", DialogResult = DialogResult.Cancel, Left = 256, Top = 80, Width = 100 };
            AcceptButton = ok;
            CancelButton = cancel;

            Controls.AddRange(new Control[] { label, _input, ok, cancel });
        }
    }
}
