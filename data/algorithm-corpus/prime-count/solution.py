n=int(input()); prime=bytearray(b'\x01')*(n+1); prime[0]=prime[1]=0
for p in range(2,int(n**0.5)+1):
    if prime[p]: prime[p*p:n+1:p]=b'\x00'*((n-p*p)//p+1)
print(sum(prime))
