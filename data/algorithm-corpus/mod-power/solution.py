a,b,p=map(int,input().split()); a%=p; ans=1%p
while b:
    if b&1: ans=ans*a%p
    a=a*a%p; b//=2
print(ans)
