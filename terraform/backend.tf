terraform {
  backend "s3" {
    bucket         = "openclaw-terraform-state-167595588574"
    key            = "openclaw/terraform.tfstate"
    region         = "us-east-2"
    dynamodb_table = "openclaw-terraform-locks"
    encrypt        = true
    profile        = "167595588574_AdministratorAccess"
  }
}
